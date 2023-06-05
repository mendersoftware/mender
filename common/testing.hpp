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

#ifndef MENDER_COMMON_TESTING
#define MENDER_COMMON_TESTING

#include <string>

#include <gtest/gtest.h>

#include <common/events.hpp>

namespace mender {
namespace common {
namespace testing {

using namespace std;

class TemporaryDirectory {
public:
	TemporaryDirectory();
	~TemporaryDirectory();

	std::string Path();

private:
	std::string path_;
};

// An event loop which automatically times out after a given amount of time.
class TestEventLoop : public mender::common::events::EventLoop {
public:
	TestEventLoop(chrono::seconds seconds = chrono::seconds(5)) :
		timer_(*this) {
		timer_.AsyncWait(seconds, [this](error_code ec) {
			Stop();
			// Better to throw exception than FAIL(), since we want to escape the caller
			// as well.
			throw runtime_error("Test timed out");
		});
	}

private:
	mender::common::events::Timer timer_;
};

::testing::AssertionResult FileContains(const string &filename, const string &expected_content);
::testing::AssertionResult FileJsonEquals(const string &filename, const string &expected_content);
::testing::AssertionResult FilesEqual(const string &filename1, const string &filename2);

class RedirectStreamOutputs {
public:
	RedirectStreamOutputs() {
		cout_stream_ = cout.rdbuf(cout_string_.rdbuf());
		cerr_stream_ = cerr.rdbuf(cerr_string_.rdbuf());
	}
	~RedirectStreamOutputs() {
		cout.rdbuf(cout_stream_);
		cerr.rdbuf(cerr_stream_);
	}

	string GetCout() const {
		return cout_string_.str();
	}

	string GetCerr() const {
		return cerr_string_.str();
	}

private:
	streambuf *cout_stream_;
	streambuf *cerr_stream_;
	stringstream cout_string_;
	stringstream cerr_string_;
};

} // namespace testing
} // namespace common
} // namespace mender

#endif // MENDER_COMMON_TESTING
