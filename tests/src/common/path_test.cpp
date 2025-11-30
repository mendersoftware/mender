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

#include <gtest/gtest.h>
#include <common/error.hpp>
#include <common/path.hpp>
#include <common/expected.hpp>

namespace error = mender::common::error;
namespace path = mender::common::path;
namespace expected = mender::common::expected;
using namespace std;


#define EXPECT_TRUE_NO_ERROR(exp)                \
	ASSERT_TRUE(exp.has_value()) << exp.error(); \
	EXPECT_TRUE(exp.value());

#define EXPECT_FALSE_NO_ERROR(exp)               \
	ASSERT_TRUE(exp.has_value()) << exp.error(); \
	EXPECT_FALSE(exp.value());

TEST(Path, IsWithinOrEqual) {
	// Test equal dirs, with "/" suffix and without
	EXPECT_TRUE_NO_ERROR(path::IsWithinOrEqual("/path/to/dir", "/path/to/dir"));
	EXPECT_TRUE_NO_ERROR(path::IsWithinOrEqual("/path/to/dir", "/path/to/dir/"));

	EXPECT_TRUE_NO_ERROR(path::IsWithinOrEqual("/path/to/dir/", "/path/to/dir"));
	EXPECT_TRUE_NO_ERROR(path::IsWithinOrEqual("/path/to/dir/", "/path/to/dir/"));

	// Test files inside dir and subdirs of dir
	EXPECT_TRUE_NO_ERROR(path::IsWithinOrEqual("/path/to/dir/module_name", "/path/to/dir"));
	EXPECT_TRUE_NO_ERROR(path::IsWithinOrEqual("/path/to/dir/module_name", "/path/to/dir/"));

	EXPECT_TRUE_NO_ERROR(path::IsWithinOrEqual("/path/to/dir/subdir/module_name", "/path/to/dir"));
	EXPECT_TRUE_NO_ERROR(path::IsWithinOrEqual("/path/to/dir/subdir/module_name", "/path/to/dir/"));

	EXPECT_TRUE_NO_ERROR(path::IsWithinOrEqual("/path/to/dir/subdir/", "/path/to/dir"));
	EXPECT_TRUE_NO_ERROR(path::IsWithinOrEqual("/path/to/dir/subdir/", "/path/to/dir/"));

	EXPECT_TRUE_NO_ERROR(path::IsWithinOrEqual("/path/to/dir/../dir/module_name", "/path/to/dir"));
	EXPECT_TRUE_NO_ERROR(path::IsWithinOrEqual("/path/to/dir/../dir/module_name", "/path/to/dir/"));

	// Test files/dirs that are outside dir
	EXPECT_FALSE_NO_ERROR(path::IsWithinOrEqual("/path/to/dir/../module_name", "/path/to/dir"));
	EXPECT_FALSE_NO_ERROR(path::IsWithinOrEqual("/path/to/dir/../module_name", "/path/to/dir/"));

	EXPECT_FALSE_NO_ERROR(path::IsWithinOrEqual("/path/to/dir/../../module_name", "/path/to/dir"));
	EXPECT_FALSE_NO_ERROR(path::IsWithinOrEqual("/path/to/dir/../../module_name", "/path/to/dir/"));

	EXPECT_FALSE_NO_ERROR(path::IsWithinOrEqual("/path/to/dir/../", "/path/to/dir"));
	EXPECT_FALSE_NO_ERROR(path::IsWithinOrEqual("/path/to/dir/../", "/path/to/dir/"));

	EXPECT_FALSE_NO_ERROR(path::IsWithinOrEqual("/path/to/dir/..", "/path/to/dir"));
	EXPECT_FALSE_NO_ERROR(path::IsWithinOrEqual("/path/to/dir/..", "/path/to/dir/"));

	EXPECT_FALSE_NO_ERROR(path::IsWithinOrEqual("/path/to/dir/../test/", "/path/to/dir"));
	EXPECT_FALSE_NO_ERROR(path::IsWithinOrEqual("/path/to/dir/../test/", "/path/to/dir/"));

	// Test completely different paths
	EXPECT_FALSE_NO_ERROR(path::IsWithinOrEqual("/completely/different/path", "/path/to/dir"));
	EXPECT_FALSE_NO_ERROR(path::IsWithinOrEqual("/completely/different/path", "/path/to/dir/"));

	EXPECT_FALSE_NO_ERROR(path::IsWithinOrEqual("/completely/different/path/", "/path/to/dir"));
	EXPECT_FALSE_NO_ERROR(path::IsWithinOrEqual("/completely/different/path/", "/path/to/dir/"));
}