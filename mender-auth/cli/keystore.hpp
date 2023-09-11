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

#ifndef MENDER_AUTH_KEYSTORE_HPP
#define MENDER_AUTH_KEYSTORE_HPP

#include <memory>
#include <string>

#include <common/error.hpp>
#include <common/crypto.hpp>

namespace mender {
namespace auth {
namespace cli {

namespace error = mender::common::error;
namespace crypto = mender::common::crypto;

using namespace std;

const int MENDER_DEFAULT_KEY_LENGTH = 3072;

enum KeyStoreErrorCode {
	NoError = 0,
	NoKeysError,
	StaticKeyError,
};

class KeyStoreErrorCategoryClass : public std::error_category {
public:
	const char *name() const noexcept override;
	string message(int code) const override;
};
extern const KeyStoreErrorCategoryClass KeyStoreErrorCategory;

error::Error MakeError(KeyStoreErrorCode code, const string &msg);

enum class StaticKey {
	No,
	Yes,
};

class MenderKeyStore {
public:
	MenderKeyStore(
		const string &key_name,
		const string &ssl_engine,
		StaticKey static_key,
		const string &passphrase) :
		key_name_ {key_name},
		ssl_engine_ {ssl_engine},
		static_key_ {static_key},
		passphrase_ {passphrase} {};

	error::Error Load();
	error::Error Save();
	error::Error Generate();

private:
	string key_name_;
	string ssl_engine_; // TODO: To be implemented as part of MEN-6668
	StaticKey static_key_;
	string passphrase_;
	unique_ptr<crypto::PrivateKey> key_;
};

} // namespace cli
} // namespace auth
} // namespace mender

#endif // MENDER_AUTH_KEYSTORE_HPP
