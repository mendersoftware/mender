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
#include <common/testing.hpp>

#include <fstream>


using namespace std;
namespace error = mender::common::error;

class LogTestEnv : public testing::Test {
protected:
	mender::common::log::Logger logger = mender::common::log::Logger("TestLogger");

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
		<< "LogLevel: " << ToStringLogLevel(log::Level());
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
		<< "LogLevel: " << ToStringLogLevel(log::Level());
	EXPECT_THAT(output, testing::HasSubstr("test=\"ing\""));
}

TEST_F(LogTestEnv, LoggerLevelFilters) {
	namespace log = mender::common::log;
	ASSERT_EQ(log::Level(), log::LogLevel::Info);

	auto sublogger = log::WithFields(log::LogField("foo", "bar"), log::LogField {"test", "ing"});
	sublogger.SetLevel(log::LogLevel::Error);

	log::SetLevel(log::LogLevel::Debug);

	testing::internal::CaptureStderr();
	log::Info("Foobar");
	sublogger.Info("BarBaz");
	auto output = testing::internal::GetCapturedStderr();
	EXPECT_GT(output.size(), 0) << "Output is: " << output;
	EXPECT_THAT(output, testing::Not(testing::HasSubstr("foo=\"bar\"")))
		<< "LogLevel: " << ToStringLogLevel(log::Level());
	EXPECT_THAT(output, testing::Not(testing::HasSubstr("test=\"ing\"")));
	EXPECT_THAT(output, testing::Not(testing::HasSubstr("BarBaz")));
	EXPECT_THAT(output, testing::HasSubstr("Foobar"));
}

class FileLogTestEnv : public LogTestEnv {
protected:
	mender::common::testing::TemporaryDirectory logs_dir;
};

TEST_F(FileLogTestEnv, SetupFileLogging) {
	namespace log = mender::common::log;
	log::SetupFileLogging(logs_dir.Path() + "/test.log");

	testing::internal::CaptureStderr();

	log::Info("info test message");
	log::Error("error test message");

	auto output = testing::internal::GetCapturedStderr();
	EXPECT_EQ(output, "");

	std::ifstream test_log(logs_dir.Path() + "/test.log");
	std::string line;
	std::getline(test_log, line);
	EXPECT_THAT(line, testing::HasSubstr("severity=info"));
	EXPECT_THAT(line, testing::HasSubstr(R"(msg="info test message")"));
	std::getline(test_log, line);
	EXPECT_THAT(line, testing::HasSubstr("severity=error"));
	EXPECT_THAT(line, testing::HasSubstr(R"(msg="error test message")"));
}

TEST(BadFileLogTest, SetupBadFileLogging) {
	namespace log = mender::common::log;
	auto err = log::SetupFileLogging("/no/such/dir/test.log");
	ASSERT_NE(error::NoError, err);
	EXPECT_EQ(err.code, log::MakeError(log::LogErrorCode::LogFileError, "").code);
}
