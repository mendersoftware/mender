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

#ifndef MENDER_UPDATE_CLI_HPP
#define MENDER_UPDATE_CLI_HPP

#include <mender-update/cli/actions.hpp>
#include <mender-update/context.hpp>

namespace mender {
namespace update {
namespace cli {

using namespace std;

ExpectedActionPtr ParseUpdateArguments(
	vector<string>::const_iterator start, vector<string>::const_iterator end);

// Use `test_hook` to modify the context during tests that test the command line directly.
int Main(
	const vector<string> &args,
	function<void(mender::update::context::MenderContext &ctx)> test_hook =
		[](mender::update::context::MenderContext &ctx) {});

} // namespace cli
} // namespace update
} // namespace mender

#endif // MENDER_UPDATE_CLI_HPP
