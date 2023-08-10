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

#include <mender-auth/actions.hpp>
#include <mender-auth/context.hpp>

#include <common/conf.hpp>
#include <common/path.hpp>

namespace mender {
namespace auth {
namespace actions {

using namespace std;

namespace conf = mender::common::conf;
namespace path = mender::common::path;


DaemonAction::DaemonAction(unique_ptr<crypto::PrivateKey> &&private_key) :
	private_key_(move(private_key)) {
}

ExpectedActionPtr DaemonAction::Create(const string &passphrase) {
	string pem_file = path::Join(conf::paths::DefaultDataStore, conf::paths::DefaultKeyFile);
	auto ex_private_key = crypto::PrivateKey::LoadFromPEM(pem_file, passphrase);
	if (ex_private_key) {
		return make_shared<DaemonAction>(move(ex_private_key.value()));
	}
	return expected::unexpected(ex_private_key.error());
}

error::Error DaemonAction::Execute(context::MenderContext &main_context) {
	return error::MakeError(error::ProgrammingError, "Not implemented...");
}

} // namespace actions
} // namespace auth
} // namespace mender
