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

#ifndef MENDER_COMMON_CRYPTO_HPP
#define MENDER_COMMON_CRYPTO_HPP

#include <cstdint>
#include <string>
#include <vector>

#include <common/expected.hpp>
#include <artifact/sha/sha.hpp>

namespace mender {
namespace common {
namespace crypto {

using namespace std;

namespace sha = mender::sha;

const int MENDER_DEFAULT_RSA_EXPONENT = 0x10001;

enum CryptoErrorCode {
	NoError = 0,
	SetupError,
	Base64Error,
	VerificationError,
};

struct Args {
	string private_key_path;
	string private_key_passphrase;
	string ssl_engine;
};

class PrivateKey;
using ExpectedPrivateKey = expected::expected<PrivateKey, error::Error>;

#ifdef MENDER_CRYPTO_OPENSSL
class OpenSSLResourceHandle;
using ResourceHandlePtr = unique_ptr<OpenSSLResourceHandle, void (*)(OpenSSLResourceHandle *)>;
using PkeyPtr = unique_ptr<EVP_PKEY, void (*)(EVP_PKEY *)>;
#endif // MENDER_CRYPTO_OPENSSL

class PrivateKey {
public:
	PrivateKey() {};

	static ExpectedPrivateKey Load(const Args &args);
	static ExpectedPrivateKey Generate(const unsigned int bits, const unsigned int exponent);
	static ExpectedPrivateKey Generate(const unsigned int bits) {
		return PrivateKey::Generate(bits, MENDER_DEFAULT_RSA_EXPONENT);
	};
	error::Error SaveToPEM(const string &private_key_path);

#ifdef MENDER_CRYPTO_OPENSSL
	PkeyPtr key {nullptr, [](EVP_PKEY *) { return; }};

	ResourceHandlePtr resource_handle_ {nullptr, [](OpenSSLResourceHandle *) { return; }};

	EVP_PKEY *Get() {
		return key.get();
	}

	operator bool() {
		return key != nullptr;
	}

	PrivateKey(PkeyPtr &&private_key) :
		key(std::move(private_key)) {};

	PrivateKey(PkeyPtr &&private_key, ResourceHandlePtr &&resource_handle) :
		key {std::move(private_key)},
		resource_handle_(std::move(resource_handle)) {};

#endif // MENDER_CRYPTO_OPENSSL
};

class CryptoErrorCategoryClass : public std::error_category {
public:
	const char *name() const noexcept override;
	string message(int code) const override;
};
extern const CryptoErrorCategoryClass CryptoErrorCategory;

error::Error MakeError(CryptoErrorCode code, const string &msg);

expected::ExpectedString ExtractPublicKey(const Args &args);

expected::ExpectedString EncodeBase64(vector<uint8_t> to_encode);

expected::ExpectedBytes DecodeBase64(string to_decode);

expected::ExpectedString Sign(const Args &args, const vector<uint8_t> &raw_data);

expected::ExpectedBool VerifySign(
	const string &public_key_path, const sha::SHA &shasum, const string &signature);

} // namespace crypto
} // namespace common
} // namespace mender



#endif // MENDER_COMMON_CRYPTO_HPP
