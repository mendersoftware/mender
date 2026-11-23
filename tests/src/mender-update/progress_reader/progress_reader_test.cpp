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

#include <mender-update/progress_reader/progress_reader.hpp>

#include <algorithm>
#include <cstdlib>
#include <ctime>
#include <iostream>
#include <vector>

#include <common/io.hpp>

#include <gtest/gtest.h>

namespace io = mender::common::io;

namespace progress = mender::update::progress;

TEST(ProgressReaderTests, RegularRead) {
	std::srand(static_cast<unsigned int>(time(nullptr)));

	std::vector<uint8_t> data(1024 * 100);

	std::generate_n(data.begin(), 1024 * 100, std::ref(std::rand));

	std::string d {data.begin(), data.end()};

	std::shared_ptr<io::StringReader> rdr = std::make_shared<io::StringReader>(d);

	auto reader = progress::Reader(rdr, 1024 * 100, true);

	testing::internal::CaptureStderr();

	// Read < 1 %
	std::vector<uint8_t> tmp(1024 * 100);
	ASSERT_TRUE(reader.Read(tmp.begin(), std::next(tmp.begin(), 10)));

	// Read 5%
	ASSERT_TRUE(reader.Read(tmp.begin(), std::next(tmp.begin(), 5 * 1024)));

	// Read < 1%
	ASSERT_TRUE(reader.Read(tmp.begin(), std::next(tmp.begin(), 10)));

	// Read 25%
	ASSERT_TRUE(reader.Read(tmp.begin(), std::next(tmp.begin(), 20 * 1024)));

	// Read 90%
	ASSERT_TRUE(reader.Read(tmp.begin(), std::next(tmp.begin(), 65 * 1024)));

	// Read 100%
	ASSERT_TRUE(reader.Read(tmp.begin(), std::next(tmp.begin(), 10 * 1024)));

	std::string output = testing::internal::GetCapturedStderr();

	EXPECT_EQ(output, "\r0%\r5%\r25%\r90%\r100%\n");
}

TEST(ProgressReaderTests, LogsEveryStepWhenNotOnTerminal) {
	const size_t size {1024 * 100};
	auto rdr = std::make_shared<io::StringReader>(std::string(size, 'x'));

	auto reader = progress::Reader(rdr, size, false);

	testing::internal::CaptureStderr();

	std::vector<uint8_t> tmp(size);
	for (size_t percent : {5, 4, 16, 65, 10}) {
		ASSERT_TRUE(reader.Read(tmp.begin(), std::next(tmp.begin(), percent * 1024)));
	}

	std::string output = testing::internal::GetCapturedStderr();

	EXPECT_EQ(output.find('\r'), std::string::npos);
	EXPECT_EQ(output.find("Payload progress: 5%"), std::string::npos);
	EXPECT_EQ(output.find("Payload progress: 9%"), std::string::npos);
	EXPECT_NE(output.find("Payload progress: 25%"), std::string::npos);
	EXPECT_NE(output.find("Payload progress: 90%"), std::string::npos);
	EXPECT_NE(output.find("Payload progress: 100%"), std::string::npos);
	EXPECT_EQ(std::count(output.begin(), output.end(), '\n'), 3);
}
