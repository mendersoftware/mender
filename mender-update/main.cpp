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

#include <memory>
#include <iostream>

using namespace std;

#include <common/error.hpp>
#include <common/expected.hpp>
#include <common/json.hpp>
#include <common/log.hpp>

using namespace mender::common;


#include <iostream>
#include <cstddef>
#include <bitset>

void log_poc() {
	namespace log = mender::common::log;

	log::Setup();

	auto logger = log::Logger("NamedLogger");

	logger.Log(log::LogLevel::Info, "Test log");

	auto sub_logger = logger.WithFields(log::LogField("foo", "bar"), log::LogField("bar", "baz"));

	sub_logger.Log(log::LogLevel::Error, "Some error");

	logger.Info("Some info message");

	logger.SetLevel(log::LogLevel::Warning);

	logger.Info("I should never show up");

	logger.SetLevel(log::LogLevel::Debug);

	logger.Log(log::LogLevel::Trace, "I should not show");
	logger.Log(log::LogLevel::Debug, "I should show test");

	// Global logger

	log::SetLevel(log::LogLevel::Info);

	log::WithFields(log::LogField("test", "ing")).Info("Bugs bunny");
	log::Info("Foobar");
	log::Warning("Hur-dur");
}

int main() {
	log_poc();

	namespace ExampleErrorType = mender::common::json;
	using ExpectedExampleString = mender::common::expected::Expected<string, error::Error>;

	ExpectedExampleString ex_s = ExpectedExampleString("Hello, world!");

	auto err = ExampleErrorType::MakeError(json::KeyError, "Something wrong happened");
	ExpectedExampleString ex_s_err = ExpectedExampleString(err);

	if (ex_s) {
		std::cout << "Got expected string value: '" << ex_s.value() << "'" << std::endl;
	} else {
		std::cout << "Got (un)expected error: '" << ex_s.error().message << "'" << std::endl;
	}

	if (ex_s_err) {
		std::cout << "Got expected string value: '" << ex_s_err.value() << "'" << std::endl;
	} else {
		std::cout << "Got (un)expected error: '" << ex_s_err.error().message << "'" << std::endl;
	}

	return 0;
}
