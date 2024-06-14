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

#ifndef MENDER_AUTH_ACTIONS_HPP
#define MENDER_AUTH_ACTIONS_HPP

#include <string>
#include <memory>

#include <mender-auth/context.hpp>
#include <mender-auth/cli/keystore.hpp>

#include <client_shared/conf.hpp>
#include <common/crypto.hpp>
#include <common/error.hpp>
#include <common/expected.hpp>

namespace mender {
namespace auth {
namespace cli {

using namespace std;

namespace conf = mender::client_shared::conf;
namespace context = mender::auth::context;
namespace crypto = mender::common::crypto;
namespace error = mender::common::error;
namespace expected = mender::common::expected;

class Action {
public:
	virtual ~Action() {};

	virtual error::Error Execute(context::MenderContext &main_context) = 0;
};
using ActionPtr = shared_ptr<Action>;
using ExpectedActionPtr = expected::expected<ActionPtr, error::Error>;

shared_ptr<MenderKeyStore> KeystoreFromConfig(
	const conf::MenderConfig &config, const string &passphrase);

#ifdef MENDER_USE_DBUS
class DaemonAction : virtual public Action {
public:
	static ExpectedActionPtr Create(
		const conf::MenderConfig &config, const string &passphrase, const bool force_bootstrap);
	error::Error Execute(context::MenderContext &main_context) override;
	DaemonAction(shared_ptr<MenderKeyStore> keystore, const bool force_bootstrap);

private:
	shared_ptr<MenderKeyStore> keystore_;
	bool force_bootstrap_;
};
#endif // MENDER_USE_DBUS

class BootstrapAction : virtual public Action {
public:
	static ExpectedActionPtr Create(
		const conf::MenderConfig &config, const string &passphrase, const bool force_bootstrap);
	error::Error Execute(context::MenderContext &main_context) override;
	BootstrapAction(shared_ptr<MenderKeyStore> keystore, const bool force_bootstrap);

private:
	shared_ptr<MenderKeyStore> keystore_;
	bool force_bootstrap_;
};

} // namespace cli
} // namespace auth
} // namespace mender

#endif // MENDER_AUTH_ACTIONS_HPP
