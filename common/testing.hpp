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

#include <memory>
#include <string>

#include <gtest/gtest.h>

#include <common/events.hpp>
#include <common/http.hpp>

namespace mender {
namespace common {
namespace testing {

using namespace std;

namespace http = mender::http;

shared_ptr<ostream> AssertInDeathTestHelper(const char *func, const char *file, int line);

// For unknown reasons, all test assertion output is disabled inside death test sub processes. It's
// extremely annoying to debug, because all errors will seem to come from the hook that invoked the
// death test sub method, and no additional output is printed. So make our own assert which aborts
// instead. Return a stream so that we can pipe into it as well, like we can with the Googletest
// asserts. Note two things in particular:
//
// 1. The use of an empty block when expression is true. This is to prevent streaming functions from
//    being called in that case, since they may not work then, for example for
//    `ASSERT_IN_DEATH_TEST(expected_reader) << expected_reader.error()`.
//
// 2. The usage of brace-less else. This is so that we can use the rest of the line to stream into
//    the helper without closing it with a brace.
//
#define ASSERT_IN_DEATH_TEST(EXPR) \
	if (EXPR) {                    \
	} else                         \
		*::mender::common::testing::AssertInDeathTestHelper(__PRETTY_FUNCTION__, __FILE__, __LINE__)

class TemporaryDirectory {
public:
	TemporaryDirectory();
	~TemporaryDirectory();

	std::string Path() const;

	void CreateSubDirectory(const string &dirname);

private:
	std::string path_;
};

// An event loop which automatically times out after a given amount of time.
class TestEventLoop : public mender::common::events::EventLoop {
public:
	TestEventLoop(chrono::seconds seconds = chrono::seconds(5)) :
		timer_ {*this} {
		timer_.AsyncWait(seconds, [this](error::Error err) {
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

class HttpFileServer {
public:
	HttpFileServer(const string &dir);
	~HttpFileServer();

	const string &GetBaseUrl() const {
		return serve_address_;
	}

	void Serve(http::ExpectedIncomingRequestPtr req);

private:
	static const string serve_address_;

	string dir_;
	thread thread_;
	events::EventLoop loop_;
	http::Server server_;
};

} // namespace testing
} // namespace common
} // namespace mender

#endif // MENDER_COMMON_TESTING
