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

#ifndef MENDER_UPDATE_UPDATE_MODULE_HPP
#define MENDER_UPDATE_UPDATE_MODULE_HPP

#include <common/error.hpp>
#include <common/expected.hpp>

namespace mender {
namespace update {
namespace update_module {
namespace v3 {

namespace error = mender::common::error;
namespace expected = mender::common::expected;

enum class RebootAction { No, Automatic, Yes };

using ExpectedRebootAction = expected::expected<RebootAction, error::Error>;

class UpdateModule {
public:
	// UpdateModule(const &artifact::Artifact artifact) : artifact_{artifact} { };

	error::Error InstallUpdate();
	ExpectedRebootAction NeedsReboot();
	error::Error Reboot();
	error::Error CommitUpdate();
	expected::ExpectedBool SupportsRollback();
	error::Error Rollback();
	error::Error VerifyReboot();
	error::Error RollbackReboot();
	error::Error VerifyRollbackReboot();
	error::Error Failure();
	error::Error Cleanup();

private:
	// Artifact artifact_
};

} // namespace v3
} // namespace update_module
} // namespace update
} // namespace mender

#endif // MENDER_UPDATE_UPDATE_MODULE_HPP
