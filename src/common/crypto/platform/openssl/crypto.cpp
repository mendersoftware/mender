// Copyright 2023 Northern.tech AS
//
//    Licensed under the Apache License, Version 2.0 (the "License");
//    you may not use this file except in compliance with the License.
//    You may obtain a copy of the License at
//
//        http://www.apache.org/licenses/LICENSE-2.0
//
//    Unless required by applicable law or agreed to in writing, software
//    distributed under the License is distributed on an "AS IS" BASIS,
//    WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//    See the License for the specific language governing permissions and
//    limitations under the License.

#include <common/crypto.hpp>

#include <common/crypto/platform/openssl/openssl_config.h>

#include <cstdint>
#include <string>
#include <vector>
#include <memory>

#include <openssl/bn.h>
#include <openssl/ecdsa.h>
#include <openssl/err.h>
#include <openssl/engine.h>
#include <openssl/ui.h>
#include <openssl/ssl.h>
#ifndef MENDER_CRYPTO_OPENSSL_LEGACY
#include <openssl/provider.h>
#include <openssl/store.h>
#endif // MENDER_CRYPTO_OPENSSL_LEGACY

#include <openssl/evp.h>
#include <openssl/conf.h>
#include <openssl/pem.h>
#include <openssl/rsa.h>

#include <common/io.hpp>
#include <common/error.hpp>
#include <common/expected.hpp>
#include <common/common.hpp>

#include <artifact/sha/sha.hpp>


namespace mender {
namespace common {
namespace crypto {

const size_t MENDER_DIGEST_SHA256_LENGTH = 32;

const size_t OPENSSL_SUCCESS = 1;

using namespace std;

namespace error = mender::common::error;
namespace io = mender::common::io;

using EnginePtr = unique_ptr<ENGINE, void (*)(ENGINE *)>;
#ifndef MENDER_CRYPTO_OPENSSL_LEGACY
using ProviderPtr = unique_ptr<OSSL_PROVIDER, int (*)(OSSL_PROVIDER *)>;
#endif // MENDER_CRYPTO_OPENSSL_LEGACY

class OpenSSLResourceHandle {
public:
	EnginePtr engine;
};

auto resource_handle_free_func = [](OpenSSLResourceHandle *h) {
	if (h) {
		delete h;
	}
};

auto pkey_ctx_free_func = [](EVP_PKEY_CTX *ctx) {
	if (ctx) {
		EVP_PKEY_CTX_free(ctx);
	}
};
auto pkey_free_func = [](EVP_PKEY *key) {
	if (key) {
		EVP_PKEY_free(key);
	}
};
auto bio_free_func = [](BIO *bio) {
	if (bio) {
		BIO_free(bio);
	}
};
auto bio_free_all_func = [](BIO *bio) {
	if (bio) {
		BIO_free_all(bio);
	}
};
auto bn_free = [](BIGNUM *bn) {
	if (bn) {
		BN_free(bn);
	}
};
auto engine_free_func = [](ENGINE *e) {
	if (e) {
		ENGINE_free(e);
	}
};

auto password_callback = [](char *buf, int size, int rwflag, void *u) {
	// We'll only use this callback for reading passphrases, not for
	// writing them.
	assert(rwflag == 0);

	if (u == nullptr) {
		return 0;
	}

	// NB: buf is not expected to be null terminated.
	char *const pass = static_cast<char *>(u);
	strncpy(buf, pass, size);

	return static_cast<int>(strnlen(pass, size));
};


// NOTE: GetOpenSSLErrorMessage should be called upon all OpenSSL errors, as
// the errors are queued, and if not harvested, the FIFO structure of the
// queue will mean that if you just get one, you might actually get the wrong
// one.
string GetOpenSSLErrorMessage() {
	const auto sysErrorCode = errno;
	auto sslErrorCode = ERR_get_error();

	std::string errorDescription {};
	while (sslErrorCode != 0) {
		if (!errorDescription.empty()) {
			errorDescription += '\n';
		}
		errorDescription += ERR_error_string(sslErrorCode, nullptr);
		sslErrorCode = ERR_get_error();
	}
	if (sysErrorCode != 0) {
		if (!errorDescription.empty()) {
			errorDescription += '\n';
		}
		errorDescription += "System error, code=" + std::to_string(sysErrorCode);
		errorDescription += ", ";
		errorDescription += strerror(sysErrorCode);
	}
	return errorDescription;
}

ExpectedPrivateKey LoadFromHSMEngine(const Args &args) {
	log::Trace("Loading the private key from HSM");

	ENGINE_load_builtin_engines();
	auto engine = EnginePtr(ENGINE_by_id(args.ssl_engine.c_str()), engine_free_func);

	if (engine == nullptr) {
		return expected::unexpected(MakeError(
			SetupError,
			"Failed to get the " + args.ssl_engine
				+ " engine. No engine with the ID found: " + GetOpenSSLErrorMessage()));
	}
	log::Debug("Loaded the HSM engine successfully!");

	int res = ENGINE_init(engine.get());
	if (not res) {
		return expected::unexpected(MakeError(
			SetupError,
			"Failed to initialise the hardware security module (HSM): "
				+ GetOpenSSLErrorMessage()));
	}
	log::Debug("Successfully initialised the HSM engine");

	auto private_key = unique_ptr<EVP_PKEY, void (*)(EVP_PKEY *)>(
		ENGINE_load_private_key(
			engine.get(),
			args.private_key_path.c_str(),
			(UI_METHOD *) nullptr,
			nullptr /*callback_data */),
		pkey_free_func);
	if (private_key == nullptr) {
		return expected::unexpected(MakeError(
			SetupError,
			"Failed to load the private key from the hardware security module: "
				+ GetOpenSSLErrorMessage()));
	}
	log::Debug("Successfully loaded the private key from the HSM Engine: " + args.ssl_engine);

	auto handle = unique_ptr<OpenSSLResourceHandle, void (*)(OpenSSLResourceHandle *)>(
		new OpenSSLResourceHandle {std::move(engine)}, resource_handle_free_func);
	return PrivateKey(std::move(private_key), std::move(handle));
}

#ifdef MENDER_CRYPTO_OPENSSL_LEGACY
ExpectedPrivateKey LoadFrom(const Args &args) {
	log::Trace("Loading private key from file: " + args.private_key_path);
	auto private_bio_key = unique_ptr<BIO, void (*)(BIO *)>(
		BIO_new_file(args.private_key_path.c_str(), "r"), bio_free_func);
	if (private_bio_key == nullptr) {
		return expected::unexpected(MakeError(
			SetupError,
			"Failed to load the private key file " + args.private_key_path + ": "
				+ GetOpenSSLErrorMessage()));
	}

	char *passphrase = const_cast<char *>(args.private_key_passphrase.c_str());

	auto private_key = unique_ptr<EVP_PKEY, void (*)(EVP_PKEY *)>(
		PEM_read_bio_PrivateKey(private_bio_key.get(), nullptr, password_callback, passphrase),
		pkey_free_func);
	if (private_key == nullptr) {
		return expected::unexpected(MakeError(
			SetupError,
			"Failed to load the private key: " + args.private_key_path + " "
				+ GetOpenSSLErrorMessage()));
	}

	return PrivateKey(std::move(private_key));
}
#endif // MENDER_CRYPTO_OPENSSL_LEGACY

#ifndef MENDER_CRYPTO_OPENSSL_LEGACY
ExpectedPrivateKey LoadFrom(const Args &args) {
	char *passphrase = const_cast<char *>(args.private_key_passphrase.c_str());

	auto ui_method = unique_ptr<UI_METHOD, void (*)(UI_METHOD *)>(
		UI_UTIL_wrap_read_pem_callback(password_callback, 0 /* rw_flag */), UI_destroy_method);
	auto ctx = unique_ptr<OSSL_STORE_CTX, int (*)(OSSL_STORE_CTX *)>(
		OSSL_STORE_open(
			args.private_key_path.c_str(),
			ui_method.get(),
			passphrase,
			nullptr, /* OSSL_PARAM params[] */
			nullptr),
		OSSL_STORE_close);

	if (ctx == nullptr) {
		return expected::unexpected(MakeError(
			SetupError,
			"Failed to load the private key from: " + args.private_key_path
				+ " error: " + GetOpenSSLErrorMessage()));
	}

	// Go through all objects in the context till we find the first private key
	while (not OSSL_STORE_eof(ctx.get())) {
		auto info = unique_ptr<OSSL_STORE_INFO, void (*)(OSSL_STORE_INFO *)>(
			OSSL_STORE_load(ctx.get()), OSSL_STORE_INFO_free);

		if (info == nullptr) {
			log::Error(
				"Failed to load the the private key: " + args.private_key_path
				+ " trying the next object in the context: " + GetOpenSSLErrorMessage());
			continue;
		}

		const int type_info {OSSL_STORE_INFO_get_type(info.get())};
		switch (type_info) {
		case OSSL_STORE_INFO_PKEY: {
			// NOTE: get1 creates a duplicate of the pkey from the info, which can be
			// used after the info ctx is destroyed
			auto private_key = unique_ptr<EVP_PKEY, void (*)(EVP_PKEY *)>(
				OSSL_STORE_INFO_get1_PKEY(info.get()), pkey_free_func);
			if (private_key == nullptr) {
				return expected::unexpected(MakeError(
					SetupError,
					"Failed to load the private key: " + args.private_key_path
						+ " error: " + GetOpenSSLErrorMessage()));
			}

			return PrivateKey(std::move(private_key));
		}
		default:
			const string info_type_string = OSSL_STORE_INFO_type_string(type_info);
			log::Debug("Unhandled OpenSSL type: expected PrivateKey, got: " + info_type_string);
			continue;
		}
	}

	return expected::unexpected(
		MakeError(SetupError, "Failed to load the private key: " + GetOpenSSLErrorMessage()));
}
#endif // ndef MENDER_CRYPTO_OPENSSL_LEGACY

ExpectedPrivateKey PrivateKey::Load(const Args &args) {
	// Numerous internal OpenSSL functions call OPENSSL_init_ssl().
	// Therefore, in order to perform nondefault initialisation,
	// OPENSSL_init_ssl() MUST be called by application code prior to any other OpenSSL function
	// calls. See: https://docs.openssl.org/3.3/man3/OPENSSL_init_ssl/#description
	if (OPENSSL_init_ssl(0, nullptr) != OPENSSL_SUCCESS) {
		log::Warning("Error initializing libssl: " + GetOpenSSLErrorMessage());
	}
	// Load OpenSSL config
	if (CONF_modules_load_file(nullptr, nullptr, 0) != OPENSSL_SUCCESS) {
		log::Warning("Failed to load OpenSSL configuration file: " + GetOpenSSLErrorMessage());
	}

	log::Trace("Loading private key");
	if (args.ssl_engine != "") {
		return LoadFromHSMEngine(args);
	}
	return LoadFrom(args);
}

ExpectedPrivateKey PrivateKey::Generate() {
#ifdef MENDER_CRYPTO_OPENSSL_LEGACY
	auto pkey_gen_ctx = unique_ptr<EVP_PKEY_CTX, void (*)(EVP_PKEY_CTX *)>(
		EVP_PKEY_CTX_new_id(EVP_PKEY_ED25519, nullptr), pkey_ctx_free_func);
#else
	auto pkey_gen_ctx = unique_ptr<EVP_PKEY_CTX, void (*)(EVP_PKEY_CTX *)>(
		EVP_PKEY_CTX_new_from_name(nullptr, "ED25519", nullptr), pkey_ctx_free_func);
#endif // MENDER_CRYPTO_OPENSSL_LEGACY

	int ret = EVP_PKEY_keygen_init(pkey_gen_ctx.get());
	if (ret != OPENSSL_SUCCESS) {
		return expected::unexpected(MakeError(
			SetupError,
			"Failed to generate a private key. Initialization failed: "
				+ GetOpenSSLErrorMessage()));
	}
	EVP_PKEY *pkey = nullptr;
#ifdef MENDER_CRYPTO_OPENSSL_LEGACY
	ret = EVP_PKEY_keygen(pkey_gen_ctx.get(), &pkey);
#else
	ret = EVP_PKEY_generate(pkey_gen_ctx.get(), &pkey);
#endif // MENDER_CRYPTO_OPENSSL_LEGACY
	if (ret != OPENSSL_SUCCESS) {
		return expected::unexpected(MakeError(
			SetupError,
			"Failed to generate a private key. Generation failed: " + GetOpenSSLErrorMessage()));
	}

	auto private_key = unique_ptr<EVP_PKEY, void (*)(EVP_PKEY *)>(pkey, pkey_free_func);
	return PrivateKey(std::move(private_key));
}

expected::ExpectedString EncodeBase64(vector<uint8_t> to_encode) {
	// Predict the len of the decoded for later verification. From man page:
	// For every 3 bytes of input provided 4 bytes of output
	// data will be produced. If n is not divisible by 3 (...)
	// the output is padded such that it is always divisible by 4.
	const size_t predicted_len {4 * ((to_encode.size() + 2) / 3)};

	// Add space for a NUL terminator. From man page:
	// Additionally a NUL terminator character will be added
	auto buffer {vector<unsigned char>(predicted_len + 1)};

	const int64_t output_len {
		EVP_EncodeBlock(buffer.data(), to_encode.data(), static_cast<int>(to_encode.size()))};
	assert(output_len >= 0);

	if (predicted_len != static_cast<uint64_t>(output_len)) {
		return expected::unexpected(
			MakeError(Base64Error, "The predicted and the actual length differ"));
	}

	return string(buffer.begin(), buffer.end() - 1); // Remove the last zero byte
}

expected::ExpectedBytes DecodeBase64(string to_decode) {
	// Predict the len of the decoded for later verification. From man page:
	// For every 4 input bytes exactly 3 output bytes will be
	// produced. The output will be padded with 0 bits if necessary
	// to ensure that the output is always 3 bytes.
	const size_t predicted_len {3 * ((to_decode.size() + 3) / 4)};

	auto buffer {vector<unsigned char>(predicted_len)};

	const int64_t output_len {EVP_DecodeBlock(
		buffer.data(),
		common::ByteVectorFromString(to_decode).data(),
		static_cast<int>(to_decode.size()))};
	assert(output_len >= 0);

	if (predicted_len != static_cast<uint64_t>(output_len)) {
		return expected::unexpected(MakeError(
			Base64Error,
			"The predicted (" + std::to_string(predicted_len) + ") and the actual ("
				+ std::to_string(output_len) + ") length differ"));
	}

	// Subtract padding bytes. Inspired by internal OpenSSL code from:
	// https://github.com/openssl/openssl/blob/ff88545e02ab48a52952350c52013cf765455dd3/crypto/ct/ct_b64.c#L46
	for (auto it = to_decode.crbegin(); *it == '='; it++) {
		buffer.pop_back();
	}

	return buffer;
}


expected::ExpectedString ExtractPublicKey(const Args &args) {
	auto exp_private_key = PrivateKey::Load(args);
	if (!exp_private_key) {
		return expected::unexpected(exp_private_key.error());
	}

	auto bio_public_key = unique_ptr<BIO, void (*)(BIO *)>(BIO_new(BIO_s_mem()), bio_free_all_func);

	if (!bio_public_key.get()) {
		return expected::unexpected(MakeError(
			SetupError,
			"Failed to extract the public key from the private key " + args.private_key_path
				+ "):" + GetOpenSSLErrorMessage()));
	}

	int ret = PEM_write_bio_PUBKEY(bio_public_key.get(), exp_private_key.value().Get());
	if (ret != OPENSSL_SUCCESS) {
		return expected::unexpected(MakeError(
			SetupError,
			"Failed to extract the public key from the private key (" + args.private_key_path
				+ "): OpenSSL BIO write failed: " + GetOpenSSLErrorMessage()));
	}

	// NOTE: At this point we already have a public key available for extraction.
	// However, when using some providers in OpenSSL3 the external provider might
	// write the key in the old PKCS#1 format. The format is not deprecated, but
	// our older backends only understand the format if it is in the PKCS#8
	// (SubjectPublicKey) format:
	//
	// For us who don't speak OpenSSL:
	//
	// -- BEGIN RSA PUBLIC KEY -- <- PKCS#1 (old format)
	// -- BEGIN PUBLIC KEY -- <- PKCS#8 (new format - can hold different key types)


	auto evp_public_key = PkeyPtr(
		PEM_read_bio_PUBKEY(bio_public_key.get(), nullptr, nullptr, nullptr), pkey_free_func);

	if (evp_public_key == nullptr) {
		return expected::unexpected(MakeError(
			SetupError,
			"Failed to extract the public key from the private key " + args.private_key_path
				+ "):" + GetOpenSSLErrorMessage()));
	}

	auto bio_public_key_new =
		unique_ptr<BIO, void (*)(BIO *)>(BIO_new(BIO_s_mem()), bio_free_all_func);

	if (bio_public_key_new == nullptr) {
		return expected::unexpected(MakeError(
			SetupError,
			"Failed to extract the public key from the public key " + args.private_key_path
				+ "):" + GetOpenSSLErrorMessage()));
	}

	ret = PEM_write_bio_PUBKEY(bio_public_key_new.get(), evp_public_key.get());
	if (ret != OPENSSL_SUCCESS) {
		return expected::unexpected(MakeError(
			SetupError,
			"Failed to extract the public key from the private key: (" + args.private_key_path
				+ "): OpenSSL BIO write failed: " + GetOpenSSLErrorMessage()));
	}

	// Inconsistent API, this returns size_t, but the API below uses int. Should not matter for
	// key sizes though.
	int pending = static_cast<int>(BIO_ctrl_pending(bio_public_key_new.get()));
	if (pending <= 0) {
		return expected::unexpected(MakeError(
			SetupError,
			"Failed to extract the public key from bio ctrl: (" + args.private_key_path
				+ "): Zero byte key unexpected: " + GetOpenSSLErrorMessage()));
	}

	vector<uint8_t> key_vector(pending);

	size_t read = BIO_read(bio_public_key_new.get(), key_vector.data(), pending);

	if (read == 0) {
		MakeError(
			SetupError,
			"Failed to extract the public key from (" + args.private_key_path
				+ "): Zero bytes read from BIO: " + GetOpenSSLErrorMessage());
	}

	return string(key_vector.begin(), key_vector.end());
}

static expected::ExpectedBytes SignED25519(EVP_PKEY *pkey, const vector<uint8_t> &raw_data) {
	size_t sig_len;

	auto md_ctx = unique_ptr<EVP_MD_CTX, void (*)(EVP_MD_CTX *)>(EVP_MD_CTX_new(), EVP_MD_CTX_free);
	if (md_ctx == nullptr) {
		return expected::unexpected(MakeError(
			SetupError, "Failed to initialize the OpenSSL md_ctx: " + GetOpenSSLErrorMessage()));
	}

	int ret {EVP_DigestSignInit(md_ctx.get(), nullptr, nullptr, nullptr, pkey)};
	if (ret != OPENSSL_SUCCESS) {
		return expected::unexpected(MakeError(
			SetupError, "Failed to initialize the OpenSSL signature: " + GetOpenSSLErrorMessage()));
	}

	/* Calculate the required size for the signature by passing a nullptr buffer */
	ret = EVP_DigestSign(md_ctx.get(), nullptr, &sig_len, raw_data.data(), raw_data.size());
	if (ret != OPENSSL_SUCCESS) {
		return expected::unexpected(MakeError(
			SetupError,
			"Failed to find the required size of the signature buffer: "
				+ GetOpenSSLErrorMessage()));
	}

	vector<uint8_t> sig(sig_len);
	ret = EVP_DigestSign(md_ctx.get(), sig.data(), &sig_len, raw_data.data(), raw_data.size());
	if (ret != OPENSSL_SUCCESS) {
		return expected::unexpected(
			MakeError(SetupError, "Failed to sign the message: " + GetOpenSSLErrorMessage()));
	}

	// The signature may in some cases be shorter than the previously allocated
	// length (which is the max)
	sig.resize(sig_len);

	return sig;
}

expected::ExpectedBytes SignGeneric(PrivateKey &private_key, const vector<uint8_t> &digest) {
	auto pkey_signer_ctx = unique_ptr<EVP_PKEY_CTX, void (*)(EVP_PKEY_CTX *)>(
		EVP_PKEY_CTX_new(private_key.Get(), nullptr), pkey_ctx_free_func);

	if (EVP_PKEY_sign_init(pkey_signer_ctx.get()) <= 0) {
		return expected::unexpected(MakeError(
			SetupError, "Failed to initialize the OpenSSL signer: " + GetOpenSSLErrorMessage()));
	}
	if (EVP_PKEY_CTX_set_signature_md(pkey_signer_ctx.get(), EVP_sha256()) <= 0) {
		return expected::unexpected(MakeError(
			SetupError,
			"Failed to set the OpenSSL signature to sha256: " + GetOpenSSLErrorMessage()));
	}

	vector<uint8_t> signature {};

	// Set the needed signature buffer length
	size_t digestlength = MENDER_DIGEST_SHA256_LENGTH, siglength;
	if (EVP_PKEY_sign(pkey_signer_ctx.get(), nullptr, &siglength, digest.data(), digestlength)
		<= 0) {
		return expected::unexpected(MakeError(
			SetupError, "Failed to get the signature buffer length: " + GetOpenSSLErrorMessage()));
	}
	signature.resize(siglength);

	if (EVP_PKEY_sign(
			pkey_signer_ctx.get(), signature.data(), &siglength, digest.data(), digestlength)
		<= 0) {
		return expected::unexpected(
			MakeError(SetupError, "Failed to sign the digest: " + GetOpenSSLErrorMessage()));
	}

	// The signature may in some cases be shorter than the previously allocated
	// length (which is the max)
	signature.resize(siglength);

	return signature;
}

expected::ExpectedBytes SignData(const Args &args, const vector<uint8_t> &raw_data) {
	auto exp_private_key = PrivateKey::Load(args);
	if (!exp_private_key) {
		return expected::unexpected(exp_private_key.error());
	}

	log::Info("Signing with: " + args.private_key_path);

	auto key_type = EVP_PKEY_base_id(exp_private_key.value().Get());

	// ED25519 signatures need to be handled independently, because of how the
	// signature scheme is designed.
	if (key_type == EVP_PKEY_ED25519) {
		return SignED25519(exp_private_key.value().Get(), raw_data);
	}

	auto exp_shasum = mender::sha::Shasum(raw_data);
	if (!exp_shasum) {
		return expected::unexpected(exp_shasum.error());
	}
	auto digest = exp_shasum.value(); /* The shasummed data = digest in crypto world */
	log::Debug("Shasum is: " + digest.String());

	return SignGeneric(exp_private_key.value(), digest);
}

expected::ExpectedString Sign(const Args &args, const vector<uint8_t> &raw_data) {
	auto exp_signed_data = SignData(args, raw_data);
	if (!exp_signed_data) {
		return expected::unexpected(exp_signed_data.error());
	}
	vector<uint8_t> signature = exp_signed_data.value();

	return EncodeBase64(signature);
}

const size_t mender_decode_buf_size = 256;
const size_t ecdsa256keySize = 32;

// Try and decode the keys from pure binary, assuming that the points on the
// curve (r,s), have been concatenated together (r || s), and simply dumped to
// binary. Which is what we did in the `mender-artifact` tool.
// (See MEN-1740) for some insight into previous issues, and the chosen fix.
static expected::ExpectedBytes TryASN1EncodeMenderCustomBinaryECFormat(
	const vector<uint8_t> &signature,
	const mender::sha::SHA &shasum,
	std::function<BIGNUM *(const unsigned char *signature, int length, BIGNUM *_unused)>
		BinaryDecoderFn) {
	// Verify that the marshalled keys match our expectation
	const size_t assumed_signature_size {2 * ecdsa256keySize};
	if (signature.size() > assumed_signature_size) {
		return expected::unexpected(MakeError(
			SetupError,
			"Unexpected size of the signature for ECDSA. Expected 2*" + to_string(ecdsa256keySize)
				+ " bytes. Got: " + to_string(signature.size())));
	}
	auto ecSig = unique_ptr<ECDSA_SIG, void (*)(ECDSA_SIG *)>(ECDSA_SIG_new(), ECDSA_SIG_free);
	if (ecSig == nullptr) {
		return expected::unexpected(MakeError(
			SetupError,
			"Failed to allocate the structure for the ECDSA signature: "
				+ GetOpenSSLErrorMessage()));
	}

	auto r = unique_ptr<BIGNUM, void (*)(BIGNUM *)>(
		BinaryDecoderFn(signature.data(), ecdsa256keySize, nullptr /* allocate new memory for r */),
		bn_free);
	if (r == nullptr) {
		return expected::unexpected(MakeError(
			SetupError,
			"Failed to extract the r(andom) part from the ECDSA signature in the binary representation: "
				+ GetOpenSSLErrorMessage()));
	}
	auto s = unique_ptr<BIGNUM, void (*)(BIGNUM *)>(
		BinaryDecoderFn(
			signature.data() + ecdsa256keySize,
			ecdsa256keySize,
			nullptr /* allocate new memory for s */),
		bn_free);
	if (s == nullptr) {
		return expected::unexpected(MakeError(
			SetupError,
			"Failed to extract the s(ignature) part from the ECDSA signature in the binary representation: "
				+ GetOpenSSLErrorMessage()));
	}

	// Set the r&s values in the SIG struct
	// r & s now owned by ecSig
	int ret {ECDSA_SIG_set0(ecSig.get(), r.get(), s.get())};
	if (ret != OPENSSL_SUCCESS) {
		return expected::unexpected(MakeError(
			SetupError,
			"Failed to set the signature parts in the ECDSA structure: "
				+ GetOpenSSLErrorMessage()));
	}
	r.release();
	s.release();

	/* Allocate some array guaranteed to hold the DER-encoded structure */
	vector<uint8_t> der_encoded_byte_array(mender_decode_buf_size);
	unsigned char *arr_p = &der_encoded_byte_array[0];
	int len = i2d_ECDSA_SIG(ecSig.get(), &arr_p);
	if (len < 0) {
		return expected::unexpected(MakeError(
			SetupError,
			"Failed to set the signature parts in the ECDSA structure: "
				+ GetOpenSSLErrorMessage()));
	}
	/* Resize to the actual size of the DER-encoded signature */
	der_encoded_byte_array.resize(len);

	return der_encoded_byte_array;
}


expected::ExpectedBool VerifySignData(
	const string &public_key_path,
	const mender::sha::SHA &shasum,
	const vector<uint8_t> &signature);

static expected::ExpectedBool VerifyECDSASignData(
	const string &public_key_path,
	const mender::sha::SHA &shasum,
	const vector<uint8_t> &signature) {
	expected::ExpectedBytes exp_der_encoded_signature =
		TryASN1EncodeMenderCustomBinaryECFormat(signature, shasum, BN_bin2bn)
			.or_else([&signature, &shasum](error::Error big_endian_error) {
				log::Debug(
					"Failed to decode the signature binary blob from our custom binary format assuming the big-endian encoding, error: "
					+ big_endian_error.String()
					+ " falling back and trying anew assuming it is little-endian encoded: ");
				return TryASN1EncodeMenderCustomBinaryECFormat(signature, shasum, BN_lebin2bn);
			});
	if (!exp_der_encoded_signature) {
		return expected::unexpected(
			MakeError(VerificationError, exp_der_encoded_signature.error().message));
	}

	vector<uint8_t> der_encoded_signature = exp_der_encoded_signature.value();

	return VerifySignData(public_key_path, shasum, der_encoded_signature);
}

static bool OpenSSLSignatureVerificationError(int a) {
	/*
	 * The signature check errored. This is different from the signature being
	 * wrong. We simply were not able to perform the check in this instance.
	 * Therefore, we fall back to trying the custom marshalled binary ECDSA
	 * signature, which we have been using in Mender.
	 */
	return a < 0;
}

expected::ExpectedBool VerifySignData(
	const string &public_key_path,
	const mender::sha::SHA &shasum,
	const vector<uint8_t> &signature) {
	auto bio_key =
		unique_ptr<BIO, void (*)(BIO *)>(BIO_new_file(public_key_path.c_str(), "r"), bio_free_func);
	if (bio_key == nullptr) {
		return expected::unexpected(MakeError(
			SetupError,
			"Failed to open the public key file from (" + public_key_path
				+ "):" + GetOpenSSLErrorMessage()));
	}

	auto pkey = unique_ptr<EVP_PKEY, void (*)(EVP_PKEY *)>(
		PEM_read_bio_PUBKEY(bio_key.get(), nullptr, nullptr, nullptr), pkey_free_func);
	if (pkey == nullptr) {
		return expected::unexpected(MakeError(
			SetupError,
			"Failed to load the public key from(" + public_key_path
				+ "): " + GetOpenSSLErrorMessage()));
	}

	auto pkey_signer_ctx = unique_ptr<EVP_PKEY_CTX, void (*)(EVP_PKEY_CTX *)>(
		EVP_PKEY_CTX_new(pkey.get(), nullptr), pkey_ctx_free_func);

	auto ret = EVP_PKEY_verify_init(pkey_signer_ctx.get());
	if (ret <= 0) {
		return expected::unexpected(MakeError(
			SetupError, "Failed to initialize the OpenSSL signer: " + GetOpenSSLErrorMessage()));
	}
	ret = EVP_PKEY_CTX_set_signature_md(pkey_signer_ctx.get(), EVP_sha256());
	if (ret <= 0) {
		return expected::unexpected(MakeError(
			SetupError,
			"Failed to set the OpenSSL signature to sha256: " + GetOpenSSLErrorMessage()));
	}

	// verify signature
	ret = EVP_PKEY_verify(
		pkey_signer_ctx.get(), signature.data(), signature.size(), shasum.data(), shasum.size());
	if (OpenSSLSignatureVerificationError(ret)) {
		log::Debug(
			"Failed to verify the signature with the supported OpenSSL binary formats. Falling back to the custom Mender encoded binary format for ECDSA signatures: "
			+ GetOpenSSLErrorMessage());
		return VerifyECDSASignData(public_key_path, shasum, signature);
	}
	if (ret == OPENSSL_SUCCESS) {
		return true;
	}
	/* This is the case where ret == 0. The signature is simply wrong */
	return false;
}

expected::ExpectedBool VerifySign(
	const string &public_key_path, const mender::sha::SHA &shasum, const string &signature) {
	// signature: decode base64
	auto exp_decoded_signature = DecodeBase64(signature);
	if (!exp_decoded_signature) {
		return expected::unexpected(exp_decoded_signature.error());
	}
	auto decoded_signature = exp_decoded_signature.value();

	return VerifySignData(public_key_path, shasum, decoded_signature);
}

error::Error PrivateKey::SaveToPEM(const string &private_key_path) {
	auto bio_key = unique_ptr<BIO, void (*)(BIO *)>(
		BIO_new_file(private_key_path.c_str(), "w"), bio_free_func);
	if (bio_key == nullptr) {
		return MakeError(
			SetupError,
			"Failed to open the private key file (" + private_key_path
				+ "): " + GetOpenSSLErrorMessage());
	}

	auto ret =
		PEM_write_bio_PrivateKey(bio_key.get(), key.get(), nullptr, nullptr, 0, nullptr, nullptr);
	if (ret != OPENSSL_SUCCESS) {
		return MakeError(
			SetupError,
			"Failed to save the private key to file (" + private_key_path
				+ "): " + GetOpenSSLErrorMessage());
	}

	return error::NoError;
}

} // namespace crypto
} // namespace common
} // namespace mender
