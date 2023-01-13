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

#include <common/log.hpp>

#include <gtest/gtest.h>
#include <gmock/gmock.h>

#include <fstream>


using namespace std;

class LogTestEnv : public testing::Test {
protected:
	mender::common::log::Logger logger = mender::common::log::Logger("TestLogger");
	static void SetUpTestSuite() {
		// Only call log::Setup() once for the test suite
		mender::common::log::Setup();
	}

	void SetUp() override {
		namespace log = mender::common::log;
		log::SetLevel(log::LogLevel::Info);
	}
};

TEST_F(LogTestEnv, SetLogLevel) {
	namespace log = mender::common::log;

	auto logger = log::Logger("TestLogger");

	EXPECT_EQ(log::LogLevel::Info, logger.Level())
		<< "Unexpected standard LogLevel - should be Info";
	logger.SetLevel(log::LogLevel::Warning);
	EXPECT_EQ(log::LogLevel::Warning, logger.Level());
}


TEST_F(LogTestEnv, GlobalLoggerSetLogLevel) {
	namespace log = mender::common::log;

	EXPECT_EQ(log::LogLevel::Info, logger.Level())
		<< "Unexpected standard LogLevel - should be Info";
	log::SetLevel(log::LogLevel::Warning);
	EXPECT_EQ(log::LogLevel::Warning, log::Level());

	log::SetLevel(log::LogLevel::Info);
}

TEST_F(LogTestEnv, LogLevelFilter) {
	namespace log = mender::common::log;
	testing::internal::CaptureStderr();
	auto logger = log::Logger("TestLogger");
	ASSERT_EQ(log::Level(), log::LogLevel::Info);

	// All log levels above and including Info
	logger.Warning("Foobar");
	logger.Error("Foobar");
	logger.Info("Foobar");
	string output = testing::internal::GetCapturedStderr();
	EXPECT_GT(output.size(), 0) << "Captured output: " << output << std::endl;

	// All log levels below Info
	testing::internal::CaptureStderr();
	logger.Trace("BarBaz");
	logger.Debug("BarBaz");
	output = testing::internal::GetCapturedStderr();
	EXPECT_EQ(output.size(), 0) << "Output is: " << output;
}

TEST_F(LogTestEnv, GlobalLoggerLogLevelFilter) {
	namespace log = mender::common::log;
	testing::internal::CaptureStderr();
	ASSERT_EQ(log::Level(), log::LogLevel::Info);

	// All log levels above and including Info
	log::Warning("Foobar");
	log::Error("Foobar");
	log::Info("Foobar");
	string output = testing::internal::GetCapturedStderr();
	EXPECT_GT(output.size(), 0) << "Captured output: " << output << std::endl;

	// All log levels below Info
	testing::internal::CaptureStderr();
	log::Trace("BarBaz");
	log::Debug("BarBaz");
	output = testing::internal::GetCapturedStderr();
	EXPECT_EQ(output.size(), 0) << "Output is: " << output;
}

TEST_F(LogTestEnv, StructuredLogging) {
	namespace log = mender::common::log;
	auto logger = log::Logger("TestLogger", log::LogLevel::Info);
	ASSERT_EQ(log::Level(), log::LogLevel::Info);

	testing::internal::CaptureStderr();
	logger.WithFields(log::LogField("foo", "bar"), log::LogField {"test", "ing"}).Info("Foobar");
	auto output = testing::internal::GetCapturedStderr();
	EXPECT_GT(output.size(), 0) << "Output is: " << output;
	EXPECT_THAT(output, testing::HasSubstr("foo=\"bar\""))
		<< "LogLevel: " << to_string_level(log::Level());
	EXPECT_THAT(output, testing::HasSubstr("test=\"ing\""));
}

TEST_F(LogTestEnv, GlobalLoggerStructuredLogging) {
	namespace log = mender::common::log;
	ASSERT_EQ(log::Level(), log::LogLevel::Info);

	testing::internal::CaptureStderr();
	log::WithFields(log::LogField("foo", "bar"), log::LogField {"test", "ing"}).Info("Foobar");
	auto output = testing::internal::GetCapturedStderr();
	EXPECT_GT(output.size(), 0) << "Output is: " << output;
	EXPECT_THAT(output, testing::HasSubstr("foo=\"bar\""))
		<< "LogLevel: " << to_string_level(log::Level());
	EXPECT_THAT(output, testing::HasSubstr("test=\"ing\""));
}
