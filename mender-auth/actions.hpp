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

#include <mender-auth/context.hpp>
#include <common/error.hpp>
#include <common/expected.hpp>
#include <common/crypto.hpp>

namespace mender {
namespace auth {
namespace actions {

using namespace std;

namespace context = mender::auth::context;
namespace error = mender::common::error;
namespace expected = mender::common::expected;
namespace crypto = mender::common::crypto;

class Action {
public:
	virtual ~Action() {};

	virtual error::Error Execute(context::MenderContext &main_context) = 0;
};
using ActionPtr = shared_ptr<Action>;
using ExpectedActionPtr = expected::expected<ActionPtr, error::Error>;

class DaemonAction : virtual public Action {
public:
	static ExpectedActionPtr Create(const string &passphrase);
	error::Error Execute(context::MenderContext &main_context) override;
	DaemonAction(unique_ptr<crypto::PrivateKey> &&private_key);

private:
	unique_ptr<crypto::PrivateKey> private_key_;
};

} // namespace actions
} // namespace auth
} // namespace mender

#endif // MENDER_AUTH_ACTIONS_HPP
