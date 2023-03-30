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

#include <mender-update/update_module/v3/update_module.hpp>

#include <common/error.hpp>
#include <common/expected.hpp>

namespace mender {
namespace update {
namespace update_module {
namespace v3 {

namespace error = mender::common::error;
namespace expected = mender::common::expected;

error::Error UpdateModule::Download() {
	return error::NoError;
}

error::Error UpdateModule::ArtifactInstall() {
	return error::NoError;
}

ExpectedRebootAction UpdateModule::NeedsReboot() {
	return ExpectedRebootAction(RebootAction::Automatic);
}

error::Error UpdateModule::ArtifactReboot() {
	return error::NoError;
}

error::Error UpdateModule::ArtifactCommit() {
	return error::NoError;
}

expected::ExpectedBool UpdateModule::SupportsRollback() {
	return expected::ExpectedBool(true);
}

error::Error UpdateModule::ArtifactRollback() {
	return error::NoError;
}

error::Error UpdateModule::ArtifactVerifyReboot() {
	return error::NoError;
}

error::Error UpdateModule::ArtifactRollbackReboot() {
	return error::NoError;
}

error::Error UpdateModule::ArtifactVerifyRollbackReboot() {
	return error::NoError;
}

error::Error UpdateModule::ArtifactFailure() {
	return error::NoError;
}

error::Error UpdateModule::Cleanup() {
	return error::NoError;
}

} // namespace v3
} // namespace update_module
} // namespace update
} // namespace mender
