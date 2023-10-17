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

#include <common/crypto/platform/openssl/openssl_config.h>

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
	const string private_key_path;
	const string private_key_passphrase;
	const string ssl_engine;
};

class PrivateKey;
using ExpectedPrivateKey = expected::expected<PrivateKey, error::Error>;

class PrivateKey {
public:
	static ExpectedPrivateKey Load(const Args &args);
	static ExpectedPrivateKey LoadFromPEM(const string &private_key_path) {
		return LoadFromPEM(private_key_path, "");
	}
	static ExpectedPrivateKey LoadFromPEM(const string &private_key_path, const string &passphrase);
	static ExpectedPrivateKey LoadFromHSM(const Args &args);
	static ExpectedPrivateKey Generate(const unsigned int bits, const unsigned int exponent);
	static ExpectedPrivateKey Generate(const unsigned int bits) {
		return PrivateKey::Generate(bits, MENDER_DEFAULT_RSA_EXPONENT);
	};
	error::Error SaveToPEM(const string &private_key_path);


#ifdef MENDER_CRYPTO_OPENSSL
	unique_ptr<EVP_PKEY, void (*)(EVP_PKEY *)> key {nullptr, [](EVP_PKEY *) { return; }};

#ifdef MENDER_CRYPTO_OPENSSL_LEGACY
	unique_ptr<ENGINE, void (*)(ENGINE *)> engine {nullptr, [](ENGINE *e) { return; }};
#else
	unique_ptr<OSSL_PROVIDER, int (*)(OSSL_PROVIDER *)> default_provider {
		nullptr, [](OSSL_PROVIDER *e) { return 0; }};
	unique_ptr<OSSL_PROVIDER, int (*)(OSSL_PROVIDER *)> hsm_provider {
		nullptr, [](OSSL_PROVIDER *e) { return 0; }};
#endif

	EVP_PKEY *Get() {
		return key.get();
	}

	PrivateKey() = default;

private:
	PrivateKey(unique_ptr<EVP_PKEY, void (*)(EVP_PKEY *)> &&private_key) :
		key(std::move(private_key)) {};

#ifdef MENDER_CRYPTO_OPENSSL_LEGACY
	PrivateKey(
		unique_ptr<EVP_PKEY, void (*)(EVP_PKEY *)> &&private_key,
		unique_ptr<ENGINE, void (*)(ENGINE *)> &&engine) :
		key(std::move(private_key)),
		engine {std::move(engine)} {};
#else
	PrivateKey(
		unique_ptr<EVP_PKEY, void (*)(EVP_PKEY *)> &&private_key,
		unique_ptr<OSSL_PROVIDER, int (*)(OSSL_PROVIDER *)> &&default_provider,
		unique_ptr<OSSL_PROVIDER, int (*)(OSSL_PROVIDER *)> &&hsm_provider) :
		key(std::move(private_key)),
		default_provider {std::move(default_provider)},
		hsm_provider {std::move(hsm_provider)} {};
#endif // MENDER_CRYPTO_OPENSSL_LEGACY
#endif // MENDER_CRYPTO_OPENSSL
};     // namespace common

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

expected::ExpectedString Sign(const Args &args, const sha::SHA &shasum);

expected::ExpectedString SignRawData(const Args &args, const vector<uint8_t> &raw_data);

expected::ExpectedBool VerifySign(
	const string &public_key_path, const sha::SHA &shasum, const string &signature);

} // namespace crypto
} // namespace common
} // namespace mender



#endif // MENDER_COMMON_CRYPTO_HPP
