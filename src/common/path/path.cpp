// Copyright 2025 Northern.tech AS
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

#include <common/path.hpp>

#include <filesystem>
#include <string>
#include <unordered_set>

#include <common/error.hpp>

namespace mender {
namespace common {
namespace path {

using namespace std;

expected::ExpectedBool IsWithinOrEqual(const string &check_path, const string &target_dir) {
	auto exp_canonical_check_path = WeaklyCanonical(check_path);
	if (!exp_canonical_check_path.has_value()) {
		return expected::unexpected(exp_canonical_check_path.error().WithContext(
			"Error creating canonical path, path to check: '" + check_path));
	}

	auto exp_canonical_target_dir = WeaklyCanonical(target_dir);
	if (!exp_canonical_target_dir.has_value()) {
		return expected::unexpected(exp_canonical_target_dir.error().WithContext(
			"Error creating canonical path, target directory: '" + target_dir));
	}

	auto canonical_check_path = exp_canonical_check_path.value();
	auto canonical_target_dir = exp_canonical_target_dir.value();

	// Terminate both with "/", otherwise we could mistakenly say that
	// 1. /test/testabc in contained within /test/test
	// 2. /test/test in not equal to /test/test/
	if (canonical_check_path.back() != '/') {
		canonical_check_path += '/';
	}
	if (canonical_target_dir.back() != '/') {
		canonical_target_dir += '/';
	}

	if (canonical_check_path.rfind(canonical_target_dir, 0) == 0) {
		return true;
	}
	return false;
}

} // namespace path
} // namespace common
} // namespace mender
