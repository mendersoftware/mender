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

#include <common/conf.hpp>

#include <vector>
#include <string>
#include <cstdlib>

#include <gtest/gtest.h>

namespace conf = mender::common::conf;

using namespace std;

TEST(ConfTests, GetEnvTest) {
	auto value = conf::GetEnv("MENDER_CONF_TEST_VAR", "default_value");
	EXPECT_EQ(value, "default_value");

	char var[] = "MENDER_CONF_TEST_VAR=mender_conf_test_value";
	int ret = putenv(var);
	ASSERT_EQ(ret, 0);

	value = conf::GetEnv("MENDER_CONF_TEST_VAR", "default_value");
	EXPECT_EQ(value, "mender_conf_test_value");
}
