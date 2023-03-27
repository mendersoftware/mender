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

#include <vector>
#include <string>

#include <common/conf.hpp>
#include <common/error.hpp>
#include <common/expected.hpp>
#include <mender-update/context.hpp>

namespace mender {
namespace update {
namespace update_module {
namespace v3 {

using namespace std;

namespace conf = mender::common::conf;
namespace context = mender::update::context;
namespace error = mender::common::error;
namespace expected = mender::common::expected;

enum class RebootAction { No, Automatic, Yes };

using ExpectedRebootAction = expected::expected<RebootAction, error::Error>;

class UpdateModule {
public:
	UpdateModule(context::MenderContext &ctx) :
		ctx_ {ctx} {};
	// UpdateModule(const context::MenderContext &ctx, const artifact::Artifact &artifact) :
	// ctx_{ctx}, artifact_{artifact} { };

	error::Error PrepareFileTree(const string &path);
	error::Error DeleteFileTree(const string &path);

	// Use same names as in Update Module specification.
	error::Error Download();
	error::Error ArtifactInstall();
	ExpectedRebootAction NeedsReboot();
	error::Error ArtifactReboot();
	error::Error ArtifactCommit();
	expected::ExpectedBool SupportsRollback();
	error::Error ArtifactRollback();
	error::Error ArtifactVerifyReboot();
	error::Error ArtifactRollbackReboot();
	error::Error ArtifactVerifyRollbackReboot();
	error::Error ArtifactFailure();
	error::Error Cleanup();

private:
	context::MenderContext &ctx_;
	// Artifact &artifact_
};

expected::ExpectedStringVector DiscoverUpdateModules(const conf::MenderConfig &config);

} // namespace v3
} // namespace update_module
} // namespace update
} // namespace mender

#endif // MENDER_UPDATE_UPDATE_MODULE_HPP
