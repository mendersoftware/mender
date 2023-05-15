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

#include <string>

namespace mender {
namespace common {
namespace crypto {


const CryptoErrorCategoryClass CryptoErrorCategory;

const char *CryptoErrorCategoryClass::name() const noexcept {
	return "CryptoErrorCategory";
}

string CryptoErrorCategoryClass::message(int code) const {
	switch (code) {
	case NoError:
		return "Success";
	case SetupError:
		return "Error during crypto library setup";
	case Base64Error:
		return "Base64 encoding error";
	default:
		return "Unknown";
	}
}

error::Error MakeError(CryptoErrorCode code, const string &msg) {
	return error::Error(error_condition(code, CryptoErrorCategory), msg);
}

} // namespace crypto
} // namespace common
} // namespace mender
