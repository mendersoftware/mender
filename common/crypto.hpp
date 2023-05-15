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

namespace mender {
namespace common {
namespace crypto {

using namespace std;


enum CryptoErrorCode {
	NoError = 0,
	SetupError,
	Base64Error,
};

class CryptoErrorCategoryClass : public std::error_category {
public:
	const char *name() const noexcept override;
	string message(int code) const override;
};
extern const CryptoErrorCategoryClass CryptoErrorCategory;

error::Error MakeError(CryptoErrorCode code, const string &msg);

expected::ExpectedString ExtractPublicKey(const string &private_key_path);

expected::ExpectedString Sign(const string private_key_path, const vector<uint8_t> raw_data);

} // namespace crypto
} // namespace common
} // namespace mender



#endif // MENDER_COMMON_CRYPTO_HPP
