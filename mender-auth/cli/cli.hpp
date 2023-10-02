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

#ifndef MENDER_AUTH_CLI_HPP
#define MENDER_AUTH_CLI_HPP

#include <vector>

#include <common/error.hpp>

#include <mender-auth/context.hpp>

namespace mender {
namespace auth {
namespace cli {

using namespace std;

namespace context = mender::auth::context;
namespace error = mender::common::error;

// Use `test_hook` to modify the context during tests that test the command line directly.
error::Error DoMain(
	const vector<string> &args,
	function<void(context::MenderContext &ctx)> test_hook = [](context::MenderContext &ctx) {});

} // namespace cli
} // namespace auth
} // namespace mender

#endif // MENDER_AUTH_CLI_HPP
