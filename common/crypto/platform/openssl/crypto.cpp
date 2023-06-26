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

#include <common/io.hpp>
#include <common/error.hpp>
#include <common/expected.hpp>
#include <common/common.hpp>

#include <artifact/sha/sha.hpp>

#include <openssl/evp.h>
#include <openssl/pem.h>
#include <openssl/err.h>
#include <openssl/rsa.h>


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

expected::ExpectedString EncodeBase64(vector<uint8_t> to_encode) {
	// Predict the len of the decoded for later verification. From man page:
	// For every 3 bytes of input provided 4 bytes of output
	// data will be produced. If n is not divisible by 3 (...)
	// the output is padded such that it is always divisible by 4.
	const auto predicted_len = 4 * ((to_encode.size() + 2) / 3);

	// Add space for a NUL terminator. From man page:
	// Additionally a NUL terminator character will be added
	auto buffer {vector<unsigned char>(predicted_len + 1)};

	const auto output_len =
		EVP_EncodeBlock(buffer.data(), to_encode.data(), static_cast<int>(to_encode.size()));

	if (predicted_len != static_cast<unsigned long>(output_len)) {
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
	const auto predicted_len = 3 * ((to_decode.size() + 3) / 4);

	auto buffer {vector<unsigned char>(predicted_len)};

	const auto output_len = EVP_DecodeBlock(
		buffer.data(),
		common::ByteVectorFromString(to_decode).data(),
		static_cast<int>(to_decode.size()));

	if (predicted_len != static_cast<unsigned long>(output_len)) {
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

string GetOpenSSLErrorMessage() {
	const auto sysErrorCode = errno;
	auto sslErrorCode = ERR_get_error();

	std::string errorDescription;
	while (sslErrorCode != 0) {
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

expected::ExpectedString ExtractPublicKey(const string &private_key_path) {
	auto private_bio_key = unique_ptr<BIO, void (*)(BIO *)>(
		BIO_new_file(private_key_path.c_str(), "r"), bio_free_func);

	if (!private_bio_key.get()) {
		return expected::unexpected(MakeError(SetupError, "Failed to open the private key file"));
	}

	auto private_key = unique_ptr<EVP_PKEY, void (*)(EVP_PKEY *)>(
		PEM_read_bio_PrivateKey(private_bio_key.get(), nullptr, nullptr, nullptr), pkey_free_func);
	if (private_key == nullptr) {
		return expected::unexpected(MakeError(SetupError, "Failed to load the key"));
	}

	auto bio_public_key = unique_ptr<BIO, void (*)(BIO *)>(BIO_new(BIO_s_mem()), bio_free_all_func);

	if (!bio_public_key.get()) {
		return expected::unexpected(MakeError(SetupError, "Failed to open the private key file"));
	}

	int ret = PEM_write_bio_PUBKEY(bio_public_key.get(), private_key.get());
	if (ret != OPENSSL_SUCCESS) {
		return expected::unexpected(MakeError(
			SetupError,
			"Failed to extract the public key. OpenSSL BIO write failed: "
				+ GetOpenSSLErrorMessage()));
	}

	int pending = BIO_ctrl_pending(bio_public_key.get());
	if (pending <= 0) {
		return expected::unexpected(
			MakeError(SetupError, "Failed to extract the public key. Zero byte key unexpected"));
	}

	vector<uint8_t> key_vector(pending);

	size_t read = BIO_read(bio_public_key.get(), key_vector.data(), pending);

	if (read <= 0) {
		MakeError(SetupError, "Failed to extract the public key. Zero bytes read from BIO");
	}

	return string(key_vector.begin(), key_vector.end());
}

expected::ExpectedBytes SignData(const string private_key_path, const vector<uint8_t> digest) {
	auto bio_private_key = unique_ptr<BIO, void (*)(BIO *)>(
		BIO_new_file(private_key_path.c_str(), "r"), bio_free_func);
	if (bio_private_key == nullptr) {
		return expected::unexpected(MakeError(SetupError, "Failed to open the private key file"));
	}

	auto pkey = unique_ptr<EVP_PKEY, void (*)(EVP_PKEY *)>(
		PEM_read_bio_PrivateKey(bio_private_key.get(), nullptr, nullptr, nullptr), pkey_free_func);
	if (pkey == nullptr) {
		return expected::unexpected(MakeError(SetupError, "Failed to load the key"));
	}

	auto pkey_signer_ctx = unique_ptr<EVP_PKEY_CTX, void (*)(EVP_PKEY_CTX *)>(
		EVP_PKEY_CTX_new(pkey.get(), nullptr), pkey_ctx_free_func);

	if (EVP_PKEY_sign_init(pkey_signer_ctx.get()) <= 0) {
		return expected::unexpected(
			MakeError(SetupError, "Failed to initialize the OpenSSL signer"));
	}
	if (EVP_PKEY_CTX_set_rsa_padding(pkey_signer_ctx.get(), RSA_PKCS1_PADDING) <= 0) {
		return expected::unexpected(
			MakeError(SetupError, "Failed to set the OpenSSL padding to RSA_PKCS1"));
	}
	if (EVP_PKEY_CTX_set_signature_md(pkey_signer_ctx.get(), EVP_sha256()) <= 0) {
		return expected::unexpected(
			MakeError(SetupError, "Failed to set the OpenSSL signature to sha256"));
	}

	vector<uint8_t> signature {};

	// Set the needed signature buffer length
	size_t digestlength = MENDER_DIGEST_SHA256_LENGTH, siglength;
	if (EVP_PKEY_sign(pkey_signer_ctx.get(), nullptr, &siglength, digest.data(), digestlength)
		<= 0) {
		return expected::unexpected(
			MakeError(SetupError, "Failed to get the signature buffer length"));
	}
	signature.resize(siglength);

	if (EVP_PKEY_sign(
			pkey_signer_ctx.get(), signature.data(), &siglength, digest.data(), digestlength)
		<= 0) {
		return expected::unexpected(MakeError(SetupError, "Failed to sign the digest"));
	}

	return signature;
}

expected::ExpectedString Sign(const string &private_key_path, const mender::sha::SHA &shasum) {
	auto exp_signed_data = SignData(private_key_path, shasum);
	if (!exp_signed_data) {
		return expected::unexpected(exp_signed_data.error());
	}
	vector<uint8_t> signature = exp_signed_data.value();

	return EncodeBase64(signature);
}

expected::ExpectedString SignRawData(
	const string &private_key_path, const vector<uint8_t> &raw_data) {
	auto exp_shasum = mender::sha::Shasum(raw_data);

	if (!exp_shasum) {
		return expected::unexpected(exp_shasum.error());
	}
	auto shasum = exp_shasum.value();
	log::Debug("Shasum is: " + shasum.String());

	return Sign(private_key_path, shasum);
}

expected::ExpectedBool VerifySignData(
	const string &public_key_path,
	const mender::sha::SHA &shasum,
	const vector<uint8_t> &signature) {
	auto bio_key =
		unique_ptr<BIO, void (*)(BIO *)>(BIO_new_file(public_key_path.c_str(), "r"), bio_free_func);
	if (bio_key == nullptr) {
		return expected::unexpected(MakeError(
			SetupError, "Failed to open the public key file: " + GetOpenSSLErrorMessage()));
	}

	auto pkey = unique_ptr<EVP_PKEY, void (*)(EVP_PKEY *)>(
		PEM_read_bio_PUBKEY(bio_key.get(), nullptr, nullptr, nullptr), pkey_free_func);
	if (pkey == nullptr) {
		return expected::unexpected(
			MakeError(SetupError, "Failed to load the key: " + GetOpenSSLErrorMessage()));
	}

	// prepare context
	auto pkey_signer_ctx = unique_ptr<EVP_PKEY_CTX, void (*)(EVP_PKEY_CTX *)>(
		EVP_PKEY_CTX_new(pkey.get(), nullptr), pkey_ctx_free_func);

	auto ret = EVP_PKEY_verify_init(pkey_signer_ctx.get());
	if (ret <= 0) {
		return expected::unexpected(MakeError(
			SetupError, "Failed to initialize the OpenSSL signer: " + GetOpenSSLErrorMessage()));
	}
	ret = EVP_PKEY_CTX_set_rsa_padding(pkey_signer_ctx.get(), RSA_PKCS1_PADDING);
	if (ret <= 0) {
		return expected::unexpected(MakeError(
			SetupError,
			"Failed to set the OpenSSL padding to RSA_PKCS1: " + GetOpenSSLErrorMessage()));
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
		return expected::unexpected(MakeError(
			VerificationError,
			"Failed to verify signature. OpenSSL PKEY verify failed: " + GetOpenSSLErrorMessage()));
	}

	return ret == 1;
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

} // namespace crypto
} // namespace common
} // namespace mender
