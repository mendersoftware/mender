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

#include <artifact/artifact.hpp>

namespace mender {
namespace update {
namespace update_module {
namespace v3 {

using namespace std;

namespace conf = mender::common::conf;
namespace context = mender::update::context;
namespace error = mender::common::error;
namespace expected = mender::common::expected;

using context::MenderContext;
using error::Error;
using expected::ExpectedBool;
using expected::ExpectedStringVector;
using mender::artifact::parser::Artifact;

enum class RebootAction { No, Automatic, Yes };

using ExpectedRebootAction = expected::expected<RebootAction, Error>;

class UpdateModule {
public:
	UpdateModule(MenderContext &ctx, artifact::PayloadHeader &update_meta_data) :
		ctx_ {ctx},
		update_meta_data_ {update_meta_data} {};

	Error PrepareFileTree(const string &path);
	Error DeleteFileTree(const string &path);

	// Use same names as in Update Module specification.
	Error Download();
	Error ArtifactInstall();
	ExpectedRebootAction NeedsReboot();
	Error ArtifactReboot();
	Error ArtifactCommit();
	ExpectedBool SupportsRollback();
	Error ArtifactRollback();
	Error ArtifactVerifyReboot();
	Error ArtifactRollbackReboot();
	Error ArtifactVerifyRollbackReboot();
	Error ArtifactFailure();
	Error Cleanup();

private:
	context::MenderContext &ctx_;
	artifact::PayloadHeader &update_meta_data_;
};

ExpectedStringVector DiscoverUpdateModules(const conf::MenderConfig &config);

} // namespace v3
} // namespace update_module
} // namespace update
} // namespace mender

#endif // MENDER_UPDATE_UPDATE_MODULE_HPP
