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

#include <cstdint>
#include <string>
#include <vector>
#include <memory>


#ifdef MENDER_CRYPTO_OPENSSL_LEGACY
#include <openssl/engine.h>
#else
#include <openssl/provider.h>
#include <openssl/store.h>
#endif // MENDER_CRYPTO_OPENSSL_LEGACY

#include <openssl/bn.h>
#include <openssl/ecdsa.h>
#include <openssl/err.h>
#include <openssl/evp.h>
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
#ifdef MENDER_CRYPTO_OPENSSL_LEGACY
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
#endif

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

ExpectedPrivateKey PrivateKey::LoadFromPEM(
	const string &private_key_path, const string &passphrase) {
	log::Trace("Loading private key from file: " + private_key_path);
	auto private_bio_key = unique_ptr<BIO, void (*)(BIO *)>(
		BIO_new_file(private_key_path.c_str(), "r"), bio_free_func);
	if (private_bio_key == nullptr) {
		return expected::unexpected(MakeError(
			SetupError,
			"Failed to open the private key file " + private_key_path + ": "
				+ GetOpenSSLErrorMessage()));
	}

	vector<char> chars(passphrase.begin(), passphrase.end());
	chars.push_back('\0');
	char *c_str = chars.data();

	// We need our own custom callback routine, as the default one will prompt
	// for a passphrase.
	auto callback = [](char *buf, int size, int rwflag, void *u) {
		// We'll only use this callback for reading passphrases, not for
		// writing them.
		assert(rwflag == 0);

		if (u == nullptr) {
			return 0;
		}

		// NB: buf is not expected to be null terminated.
		char *const pass = static_cast<char *>(u);
		strncpy(buf, pass, size);

		const int len = static_cast<int>(strlen(pass));
		return (len < size) ? len : size;
	};

	auto private_key = unique_ptr<EVP_PKEY, void (*)(EVP_PKEY *)>(
		PEM_read_bio_PrivateKey(private_bio_key.get(), nullptr, callback, c_str), pkey_free_func);
	if (private_key == nullptr) {
		return expected::unexpected(MakeError(
			SetupError,
			"Failed to load the key: " + private_key_path + " " + GetOpenSSLErrorMessage()));
	}

	return PrivateKey(std::move(private_key));
}

#ifdef MENDER_CRYPTO_OPENSSL_LEGACY
ExpectedPrivateKey PrivateKey::LoadFromHSM(const Args &args) {
	log::Trace("Loading the private key from HSM");

	ENGINE_load_builtin_engines();
	auto engine = unique_ptr<ENGINE, void (*)(ENGINE *)>(
		ENGINE_by_id(args.ssl_engine.c_str()), engine_free_func);

	if (engine == NULL) {
		return expected::unexpected(MakeError(
			SetupError,
			"Failed to get the " + args.ssl_engine
				+ " engine. No engine with the ID found: " + GetOpenSSLErrorMessage()));
	}
	log::Debug("Loaded the HSM engine successfully!");

	int res = ENGINE_init(engine.get());
	if (res == 0) {
		return expected::unexpected(MakeError(
			SetupError,
			"Failed to initialise the hardware security module (HSM): "
				+ GetOpenSSLErrorMessage()));
	}
	log::Debug("Successfully initialised the HSM engine");

	// Load the private key
	void *ui_method = NULL;
	void *callback_data = NULL;
	auto private_key = unique_ptr<EVP_PKEY, void (*)(EVP_PKEY *)>(
		ENGINE_load_private_key(
			engine.get(), args.private_key_path.c_str(), (UI_METHOD *) ui_method, callback_data),
		pkey_free_func);
	if (private_key.get() == NULL) {
		return expected::unexpected(MakeError(
			SetupError,
			"Failed to load the private key from the hardware security module: "
				+ GetOpenSSLErrorMessage()));
	}
	log::Debug("Successfully loaded the private key from the HSM Engine: " + args.ssl_engine);

	return PrivateKey(std::move(private_key), std::move(engine));
}
#endif // MENDER_CRYPTO_OPENSSL_LEGACY

#ifndef MENDER_CRYPTO_OPENSSL_LEGACY
ExpectedPrivateKey PrivateKey::LoadFromHSM(const Args &args) {
	log::Debug("Loading the private key from HSM");

	auto default_provider = unique_ptr<OSSL_PROVIDER, int (*)(OSSL_PROVIDER *)>(
		OSSL_PROVIDER_load(NULL, "default"), OSSL_PROVIDER_unload);
	if (default_provider.get() == nullptr) {
		return expected::unexpected(
			MakeError(SetupError, "default provider load error: " + GetOpenSSLErrorMessage()));
	}

	auto hsm_provider = unique_ptr<OSSL_PROVIDER, int (*)(OSSL_PROVIDER *)>(
		OSSL_PROVIDER_load(NULL, args.ssl_engine.c_str()), OSSL_PROVIDER_unload);
	if (hsm_provider.get() == nullptr) {
		return expected::unexpected(MakeError(
			SetupError, args.ssl_engine + " provider load error: " + GetOpenSSLErrorMessage()));
	}

	int ret {OSSL_PROVIDER_available(NULL, args.ssl_engine.c_str())};
	if (ret != OPENSSL_SUCCESS) {
		return expected::unexpected(MakeError(
			SetupError, args.ssl_engine + " provider not available: " + GetOpenSSLErrorMessage()));
	}

	log::Trace("Loading key: " + args.private_key_path);
	auto ctx = unique_ptr<OSSL_STORE_CTX, int (*)(OSSL_STORE_CTX *)>(
		OSSL_STORE_open(args.private_key_path.c_str(), NULL, NULL, NULL, NULL), OSSL_STORE_close);

	if (ctx == nullptr) {
		return expected::unexpected(MakeError(
			SetupError,
			"OSSL_STORE_OPEN: Failed to load the private key from the hardware security module: "
				+ GetOpenSSLErrorMessage()));
	}

	/*
	 * OSSL_STORE_eof() simulates file semantics for any repository to signal
	 * that no more data can be expected
	 */
	while (not OSSL_STORE_eof(ctx.get())) {
		OSSL_STORE_INFO *info = OSSL_STORE_load(ctx.get());

		if (info == NULL) {
			return expected::unexpected(MakeError(
				SetupError,
				"Failed to read the private key information the hardware security module: "
					+ GetOpenSSLErrorMessage()));
		}

		const int type_info {OSSL_STORE_INFO_get_type(info)};
		switch (type_info) {
		case OSSL_STORE_INFO_PKEY: {
			// NOTE: get1 creates a duplicate of the pkey from the info, which can be
			// used after the info ctx is destroyed
			auto private_key = unique_ptr<EVP_PKEY, void (*)(EVP_PKEY *)>(
				OSSL_STORE_INFO_get1_PKEY(info), pkey_free_func);
			if (private_key == nullptr) {
				log::Error(
					"Failed to load the private key from the hardware security module: "
					+ GetOpenSSLErrorMessage());
				return expected::unexpected(MakeError(
					SetupError,
					"Failed to load the private key from the hardware security module: "
						+ GetOpenSSLErrorMessage()));
			}
			log::Info("Successfully loaded!");
			return PrivateKey(
				std::move(private_key), std::move(default_provider), std::move(hsm_provider));
		}
		default:
			const string info_type_string = OSSL_STORE_INFO_type_string(type_info);
			return expected::unexpected(MakeError(
				SetupError,
				"Unhandled OpenSSL type: expected PrivateKey, got: " + info_type_string));
			break;
		}
	}

	return expected::unexpected(MakeError(
		SetupError,
		"Failed to load the private key from the hardware security module: "
			+ GetOpenSSLErrorMessage()));
}
#endif // ndef MENDER_CRYPTO_OPENSSL_LEGACY

ExpectedPrivateKey PrivateKey::Load(const Args &args) {
	log::Trace("Loading private key");
	if (args.ssl_engine != "") {
		return LoadFromHSM(args);
	}
	return LoadFromPEM(args.private_key_path);
}

ExpectedPrivateKey PrivateKey::Generate(const unsigned int bits, const unsigned int exponent) {
#ifdef MENDER_CRYPTO_OPENSSL_LEGACY
	auto pkey_gen_ctx = unique_ptr<EVP_PKEY_CTX, void (*)(EVP_PKEY_CTX *)>(
		EVP_PKEY_CTX_new_id(EVP_PKEY_RSA, nullptr), pkey_ctx_free_func);

	int ret = EVP_PKEY_keygen_init(pkey_gen_ctx.get());
	if (ret != OPENSSL_SUCCESS) {
		return expected::unexpected(MakeError(
			SetupError,
			"Failed to generate a private key. Initialization failed: "
				+ GetOpenSSLErrorMessage()));
	}

	ret = EVP_PKEY_CTX_set_rsa_keygen_bits(pkey_gen_ctx.get(), bits);
	if (ret != OPENSSL_SUCCESS) {
		return expected::unexpected(MakeError(
			SetupError,
			"Failed to generate a private key. Parameters setting failed: "
				+ GetOpenSSLErrorMessage()));
	}

	auto exponent_bn = unique_ptr<BIGNUM, void (*)(BIGNUM *)>(BN_new(), bn_free);
	ret = BN_set_word(exponent_bn.get(), exponent);
	if (ret != OPENSSL_SUCCESS) {
		return expected::unexpected(MakeError(
			SetupError,
			"Failed to generate a private key. Parameters setting failed: "
				+ GetOpenSSLErrorMessage()));
	}

	ret = EVP_PKEY_CTX_set_rsa_keygen_pubexp(pkey_gen_ctx.get(), exponent_bn.get());
	if (ret != OPENSSL_SUCCESS) {
		return expected::unexpected(MakeError(
			SetupError,
			"Failed to generate a private key. Parameters setting failed: "
				+ GetOpenSSLErrorMessage()));
	}
	exponent_bn.release();

	EVP_PKEY *pkey = nullptr;
	ret = EVP_PKEY_keygen(pkey_gen_ctx.get(), &pkey);
	if (ret != OPENSSL_SUCCESS) {
		return expected::unexpected(MakeError(
			SetupError,
			"Failed to generate a private key. Generation failed: " + GetOpenSSLErrorMessage()));
	}
#else
	auto pkey_gen_ctx = unique_ptr<EVP_PKEY_CTX, void (*)(EVP_PKEY_CTX *)>(
		EVP_PKEY_CTX_new_from_name(nullptr, "RSA", nullptr), pkey_ctx_free_func);

	int ret = EVP_PKEY_keygen_init(pkey_gen_ctx.get());
	if (ret != OPENSSL_SUCCESS) {
		return expected::unexpected(MakeError(
			SetupError,
			"Failed to generate a private key. Initialization failed: "
				+ GetOpenSSLErrorMessage()));
	}

	OSSL_PARAM params[3];
	auto bits_buffer = bits;
	auto exponent_buffer = exponent;
	params[0] = OSSL_PARAM_construct_uint("bits", &bits_buffer);
	params[1] = OSSL_PARAM_construct_uint("e", &exponent_buffer);
	params[2] = OSSL_PARAM_construct_end();

	ret = EVP_PKEY_CTX_set_params(pkey_gen_ctx.get(), params);
	if (ret != OPENSSL_SUCCESS) {
		return expected::unexpected(MakeError(
			SetupError,
			"Failed to generate a private key. Parameters setting failed: "
				+ GetOpenSSLErrorMessage()));
	}

	EVP_PKEY *pkey = nullptr;
	ret = EVP_PKEY_generate(pkey_gen_ctx.get(), &pkey);
	if (ret != OPENSSL_SUCCESS) {
		return expected::unexpected(MakeError(
			SetupError,
			"Failed to generate a private key. Generation failed: " + GetOpenSSLErrorMessage()));
	}
#endif

	auto private_key = unique_ptr<EVP_PKEY, void (*)(EVP_PKEY *)>(pkey, pkey_free_func);
	return PrivateKey(std::move(private_key));
}

expected::ExpectedString EncodeBase64(vector<uint8_t> to_encode) {
	// Predict the len of the decoded for later verification. From man page:
	// For every 3 bytes of input provided 4 bytes of output
	// data will be produced. If n is not divisible by 3 (...)
	// the output is padded such that it is always divisible by 4.
	const uint64_t predicted_len {4 * ((to_encode.size() + 2) / 3)};

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
	const uint64_t predicted_len {3 * ((to_decode.size() + 3) / 4)};

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
			"Failed to extract the public key from: (" + args.private_key_path
				+ "): OpenSSL BIO write failed: " + GetOpenSSLErrorMessage()));
	}

	int pending = BIO_ctrl_pending(bio_public_key.get());
	if (pending <= 0) {
		return expected::unexpected(MakeError(
			SetupError,
			"Failed to extract the public key from: (" + args.private_key_path
				+ "): Zero byte key unexpected: " + GetOpenSSLErrorMessage()));
	}

	vector<uint8_t> key_vector(pending);

	size_t read = BIO_read(bio_public_key.get(), key_vector.data(), pending);

	if (read == 0) {
		MakeError(
			SetupError,
			"Failed to extract the public key from (" + args.private_key_path
				+ "): Zero bytes read from BIO: " + GetOpenSSLErrorMessage());
	}

	return string(key_vector.begin(), key_vector.end());
}

expected::ExpectedBytes SignData(const Args &args, const vector<uint8_t> &digest) {
	auto exp_private_key = PrivateKey::Load(args);
	if (!exp_private_key) {
		return expected::unexpected(exp_private_key.error());
	}

	auto pkey_signer_ctx = unique_ptr<EVP_PKEY_CTX, void (*)(EVP_PKEY_CTX *)>(
		EVP_PKEY_CTX_new(exp_private_key.value().Get(), nullptr), pkey_ctx_free_func);

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

expected::ExpectedString Sign(const Args &args, const mender::sha::SHA &shasum) {
	auto exp_signed_data = SignData(args, shasum);
	if (!exp_signed_data) {
		return expected::unexpected(exp_signed_data.error());
	}
	vector<uint8_t> signature = exp_signed_data.value();

	return EncodeBase64(signature);
}

expected::ExpectedString SignRawData(const Args &args, const vector<uint8_t> &raw_data) {
	auto exp_shasum = mender::sha::Shasum(raw_data);

	if (!exp_shasum) {
		return expected::unexpected(exp_shasum.error());
	}
	auto shasum = exp_shasum.value();
	log::Debug("Shasum is: " + shasum.String());

	return Sign(args, shasum);
}

const size_t ecdsa256curveBits = 256;
const size_t ecdsa256keySize = 32;

// Try and decode the keys from pure binary, assuming that the points on the
// curve (r,s), have been concatenated together (r || s), and simply dumped to
// binary. Which is what we did in the `mender-artifact` tool.
// (See MEN-1740) for some insight into previous issues, and the chosen fix.
template <BIGNUM *DecoderFn(const unsigned char *, int, BIGNUM *)>
static expected::ExpectedBytes TryASN1EncodeMenderCustomBinaryECFormat(
	const vector<uint8_t> &signature, const mender::sha::SHA &shasum) {
	// Verify that the marshalled keys match our expectation
	const size_t assumed_signature_size {2 * ecdsa256keySize};
	if (signature.size() > assumed_signature_size) {
		return expected::unexpected(MakeError(
			SetupError,
			"Unexpected size of the signature for ECDSA. Expected 2*32 bytes. Got: "
				+ to_string(signature.size())));
	}
	auto ecSig = unique_ptr<ECDSA_SIG, void (*)(ECDSA_SIG *)>(ECDSA_SIG_new(), ECDSA_SIG_free);
	if (ecSig == nullptr) {
		return expected::unexpected(MakeError(
			SetupError,
			"Failed to allocate the structure for the ECDSA signature" + GetOpenSSLErrorMessage()));
	}

	auto r = unique_ptr<BIGNUM, void (*)(BIGNUM *)>(
		DecoderFn(signature.data(), ecdsa256keySize, NULL), BN_free);
	if (r == NULL) {
		return expected::unexpected(MakeError(
			SetupError,
			"Failed to extract the r(andom) part from the ECDSA signature in the binary representation"
				+ GetOpenSSLErrorMessage()));
	}
	auto s = unique_ptr<BIGNUM, void (*)(BIGNUM *)>(
		DecoderFn(signature.data() + ecdsa256keySize, ecdsa256keySize, NULL), BN_free);
	if (s == NULL) {
		return expected::unexpected(MakeError(
			SetupError,
			"Failed to extract the s(ignature) part from the ECDSA signature in the binary representation"
				+ GetOpenSSLErrorMessage()));
	}

	// Set the r&s values in the SIG struct
	// r & s now owned by ecSig
	int ret {ECDSA_SIG_set0(ecSig.get(), r.release(), s.release())};
	if (ret != OPENSSL_SUCCESS) {
		return expected::unexpected(MakeError(
			SetupError,
			"Failed to set the signature parts in the ECDSA structure" + GetOpenSSLErrorMessage()));
	}

	/* Allocate some array guaranteed to hold the DER-encoded structure */
	vector<uint8_t> arr(256);
	unsigned char *arr_p = &arr[0];
	int len = i2d_ECDSA_SIG(ecSig.get(), &arr_p);
	if (len < 0) {
		return expected::unexpected(MakeError(
			SetupError,
			"Failed to set the signature parts in the ECDSA structure" + GetOpenSSLErrorMessage()));
	}
	/* Resize to the actual size of the DER-encoded signature */
	arr.resize(len);

	return arr;
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
		TryASN1EncodeMenderCustomBinaryECFormat<BN_bin2bn>(signature, shasum);
	if (not exp_der_encoded_signature) {
		return expected::unexpected(
			MakeError(VerificationError, exp_der_encoded_signature.error().message));
	}

	vector<uint8_t> der_encoded_signature = exp_der_encoded_signature.value();

	return VerifySignData(public_key_path, shasum, der_encoded_signature);
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
	if (ret < 0) {
		const string openssl_error_msg {GetOpenSSLErrorMessage()};
		if (openssl_error_msg.find("asn1 encoding") != string::npos) {
			log::Debug(
				"Failed to verify the signature with the supported OpenSSL binary formats. Falling back to the custom Mender encoded binary format for ECDSA signatures");
			return VerifyECDSASignData(public_key_path, shasum, signature);
		}
		return expected::unexpected(MakeError(
			VerificationError,
			"Failed to verify the new der encoded signature. OpenSSL PKEY verify failed: "
				+ openssl_error_msg));
	}
	return ret == OPENSSL_SUCCESS;
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

	// PEM_write_bio_PrivateKey_traditional will use the key-specific PKCS1
	// format if one is available for that key type, otherwise it will encode
	// to a PKCS8 key.
	auto ret = PEM_write_bio_PrivateKey_traditional(
		bio_key.get(), key.get(), nullptr, nullptr, 0, nullptr, nullptr);
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
