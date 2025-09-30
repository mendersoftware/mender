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

#include <common/device_tier.hpp>

#include <gtest/gtest.h>

using namespace std;
namespace device_tier = mender::common::device_tier;

TEST(DeviceTierTests, ValidTiers) {
	EXPECT_TRUE(device_tier::IsValid(device_tier::kStandard));
	EXPECT_TRUE(device_tier::IsValid(device_tier::kSystem));
	EXPECT_TRUE(device_tier::IsValid(device_tier::kMicro));
}

TEST(DeviceTierTests, InvalidTiers) {
	EXPECT_FALSE(device_tier::IsValid("foobar"));
	EXPECT_FALSE(device_tier::IsValid(""));
	EXPECT_FALSE(device_tier::IsValid("Standard"));
	EXPECT_FALSE(device_tier::IsValid("System"));
	EXPECT_FALSE(device_tier::IsValid("Micro"));
	EXPECT_FALSE(device_tier::IsValid("STANDARD"));
	EXPECT_FALSE(device_tier::IsValid("SYSTEM"));
	EXPECT_FALSE(device_tier::IsValid("MICRO"));
}

TEST(DeviceTierTests, TierValues) {
	EXPECT_EQ(device_tier::kStandard, "standard");
	EXPECT_EQ(device_tier::kSystem, "system");
	EXPECT_EQ(device_tier::kMicro, "micro");
}

TEST(DeviceTierTests, EdgeCases) {
	EXPECT_FALSE(device_tier::IsValid(" standard"));
	EXPECT_FALSE(device_tier::IsValid("standard "));
	EXPECT_FALSE(device_tier::IsValid(" standard "));
	EXPECT_FALSE(device_tier::IsValid("standard\n"));
	EXPECT_FALSE(device_tier::IsValid("standard\t"));
}
