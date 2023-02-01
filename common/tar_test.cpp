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

#include <common/tar.hpp>

#include <gtest/gtest.h>
#include <gmock/gmock.h>

#include <cstdint>
#include <fstream>

using namespace std;

TEST(TarTest, TestTarReaderInitialization) {
	std::fstream fs {"../common/testdata/test.tar"};

	mender::common::io::StreamReader sr {fs};

	mender::tar::Reader tar_reader {sr};

	mender::tar::Entry tar_entry = tar_reader.Next();

	ASSERT_EQ(tar_entry.Name(), "testdata");

	vector<uint8_t> data(10);

	auto bytes_read = tar_entry.Read(data);

	ASSERT_TRUE(bytes_read);

	ASSERT_GT(bytes_read.value(), 0);

	vector<uint8_t> expected {'f', 'o', 'o', 'b', 'a', 'r', '\n', '\0', '\0', '\0'};

	ASSERT_EQ(data, expected);
}
