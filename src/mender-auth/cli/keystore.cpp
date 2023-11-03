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

#include <mender-auth/cli/keystore.hpp>

#include <string>
#include <utility>

#include <common/log.hpp>
#include <common/crypto.hpp>


namespace mender {
namespace auth {
namespace cli {

using namespace std;

namespace log = mender::common::log;

namespace crypto = mender::common::crypto;

const KeyStoreErrorCategoryClass KeyStoreErrorCategory;

const char *KeyStoreErrorCategoryClass::name() const noexcept {
	return "KeyStoreErrorCategory";
}

string KeyStoreErrorCategoryClass::message(int code) const {
	switch (code) {
	case NoError:
		return "Success";
	case NoKeysError:
		return "No key in memory";
	case StaticKeyError:
		return "Cannot replace static key";
	}
	// Don't use "default" case. This should generate a warning if we ever add any enums. But
	// still assert here for safety.
	assert(false);
	return "Unknown";
}

error::Error MakeError(KeyStoreErrorCode code, const string &msg) {
	return error::Error(error_condition(code, KeyStoreErrorCategory), msg);
}

error::Error MenderKeyStore::Load() {
	log::Trace("Loading the keystore");
	auto exp_key = crypto::PrivateKey::Load({key_name_, passphrase_, ssl_engine_});
	if (!exp_key) {
		return MakeError(
			NoKeysError,
			"Error loading private key from " + key_name_ + ": " + exp_key.error().message);
	}
	log::Info("Successfully loaded private key from " + key_name_);
	key_ = move(exp_key.value());

	return error::NoError;
}

error::Error MenderKeyStore::Save() {
	if (!key_) {
		return MakeError(NoKeysError, "Need to load or generate a key before save");
	}

	return key_.SaveToPEM(key_name_);
}

error::Error MenderKeyStore::Generate() {
	if (static_key_ == StaticKey::Yes) {
		return MakeError(StaticKeyError, "A static key cannot be re-generated");
	}

	auto exp_key = crypto::PrivateKey::Generate(MENDER_DEFAULT_KEY_LENGTH);
	if (!exp_key) {
		return exp_key.error();
	}
	key_ = std::move(exp_key.value());

	return error::NoError;
}

} // namespace cli
} // namespace auth
} // namespace mender
