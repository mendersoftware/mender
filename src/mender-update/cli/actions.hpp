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

#ifndef MENDER_UPDATE_ACTIONS_HPP
#define MENDER_UPDATE_ACTIONS_HPP

#include <common/error.hpp>
#include <common/expected.hpp>

#include <mender-update/context.hpp>

namespace mender {
namespace update {
namespace cli {

using namespace std;

namespace error = mender::common::error;
namespace expected = mender::common::expected;
namespace context = mender::update::context;

class Action {
public:
	virtual ~Action() {};

	virtual error::Error Execute(context::MenderContext &main_context) = 0;
};
using ActionPtr = shared_ptr<Action>;
using ExpectedActionPtr = expected::expected<ActionPtr, error::Error>;

class ShowArtifactAction : virtual public Action {
public:
	error::Error Execute(context::MenderContext &main_context) override;
};

class ShowProvidesAction : virtual public Action {
public:
	error::Error Execute(context::MenderContext &main_context) override;
};

class InstallAction : virtual public Action {
public:
	InstallAction(const string &src, bool reboot_exit_code) :
		src_ {src},
		reboot_exit_code_ {reboot_exit_code} {
	}

	error::Error Execute(context::MenderContext &main_context) override;

private:
	string src_;
	bool reboot_exit_code_;
};

class CommitAction : virtual public Action {
public:
	error::Error Execute(context::MenderContext &main_context) override;
};

class RollbackAction : virtual public Action {
public:
	error::Error Execute(context::MenderContext &main_context) override;
};

class DaemonAction : virtual public Action {
public:
	error::Error Execute(context::MenderContext &main_context) override;
};

class SendInventoryAction : virtual public Action {
public:
	error::Error Execute(context::MenderContext &main_context) override;
};

class CheckUpdateAction : virtual public Action {
public:
	error::Error Execute(context::MenderContext &main_context) override;
};

error::Error MaybeInstallBootstrapArtifact(context::MenderContext &main_context);

} // namespace cli
} // namespace update
} // namespace mender

#endif // MENDER_UPDATE_ACTIONS_HPP
