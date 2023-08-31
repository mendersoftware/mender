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

enum CryptoErrorCode {
	NoError = 0,
	SetupError,
	Base64Error,
	VerificationError,
};

class PrivateKey;
using ExpectedPrivateKey = expected::expected<unique_ptr<PrivateKey>, error::Error>;

class PrivateKey {
public:
	static ExpectedPrivateKey LoadFromPEM(const string &private_key_path, const string &passphrase);
	static ExpectedPrivateKey LoadFromPEM(const string &private_key_path);
	static ExpectedPrivateKey Generate(const unsigned int bits, const unsigned int exponent);
	static ExpectedPrivateKey Generate(const unsigned int bits);
	error::Error SaveToPEM(const string &private_key_path);
#ifdef MENDER_CRYPTO_OPENSSL
	unique_ptr<EVP_PKEY, void (*)(EVP_PKEY *)> key;

private:
	PrivateKey(unique_ptr<EVP_PKEY, void (*)(EVP_PKEY *)> &&private_key) :
		key(std::move(private_key)) {};
#endif // MENDER_CRYPTO_OPENSSL
};

class CryptoErrorCategoryClass : public std::error_category {
public:
	const char *name() const noexcept override;
	string message(int code) const override;
};
extern const CryptoErrorCategoryClass CryptoErrorCategory;

error::Error MakeError(CryptoErrorCode code, const string &msg);

expected::ExpectedString ExtractPublicKey(const string &private_key_path);

expected::ExpectedString EncodeBase64(vector<uint8_t> to_encode);

expected::ExpectedBytes DecodeBase64(string to_decode);

expected::ExpectedString Sign(const string &private_key_path, const sha::SHA &shasum);

expected::ExpectedString SignRawData(
	const string &private_key_path, const vector<uint8_t> &raw_data);

expected::ExpectedBool VerifySign(
	const string &public_key_path, const sha::SHA &shasum, const string &signature);

} // namespace crypto
} // namespace common
} // namespace mender



#endif // MENDER_COMMON_CRYPTO_HPP
