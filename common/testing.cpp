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

#include <common/testing.hpp>

#include <cstdlib>
#include <filesystem>
#include <random>
#include <iostream>

#include <common/json.hpp>
#include <common/log.hpp>
#include <common/path.hpp>
#include <common/processes.hpp>

namespace mender {
namespace common {
namespace testing {

namespace fs = std::filesystem;

namespace log = mender::common::log;
namespace path = mender::common::path;

shared_ptr<ostream> AssertInDeathTestHelper(const char *func, const char *file, int line) {
	// Unsuccessful assert. Return a stream which prints to stderr, and which aborts when it is
	// destroyed (at the end of the statement evaluation).
	cerr << "Assert at " << func << " in " << file << ":" << line << endl;
	return shared_ptr<ostream>(new ostream(cerr.rdbuf()), [](ostream *) { std::abort(); });
}

TemporaryDirectory::TemporaryDirectory() {
	fs::path path = fs::temp_directory_path();
	path.append("mender-test-" + std::to_string(std::random_device()()));
	fs::create_directories(path);
	path_ = path;
}

TemporaryDirectory::~TemporaryDirectory() {
	fs::remove_all(path_);
}

std::string TemporaryDirectory::Path() const {
	return path_;
}

::testing::AssertionResult FileContains(const string &filename, const string &expected_content) {
	ifstream is {filename};
	ostringstream contents_s;
	contents_s << is.rdbuf();
	string contents {contents_s.str()};
	if (contents == expected_content) {
		return ::testing::AssertionSuccess();
	}
	return ::testing::AssertionFailure()
		   << "Expected: '" << expected_content << "' Got: '" << contents << "'";
}


::testing::AssertionResult FileJsonEquals(const string &filename, const string &expected_content) {
	ifstream is {filename};
	json::Json contents = json::Load(is).value();
	json::Json expected_contents = json::Load(expected_content).value();
	if (contents.Dump() == expected_contents.Dump()) {
		return ::testing::AssertionSuccess();
	}
	return ::testing::AssertionFailure()
		   << "Expected: '" << contents.Dump() << "' Got: '" << expected_contents.Dump() << "'";
}

::testing::AssertionResult FilesEqual(const string &filename1, const string &filename2) {
	processes::Process proc({"diff", "-u", filename1, filename2});
	auto err = proc.Run();
	if (err == error::NoError) {
		return ::testing::AssertionSuccess();
	}
	// Some extra information in case of failure.
	cout << "ls -l " << filename1 << " " << filename2 << endl;
	processes::Process listdir({"ls", "-l", filename1, filename2});
	listdir.Run();
	return ::testing::AssertionFailure() << filename1 << " and " << filename2 << " differ";
}

const string HttpFileServer::serve_address_ {"http://127.0.0.1:53272"};

HttpFileServer::HttpFileServer(const string &dir) :
	dir_ {dir},
	server_(http::ServerConfig {}, loop_) {
	// The reason we need this synchronization is because of the thread sanitizer and
	// logging. AsyncServeUrl uses the logger internally, and the log level is also set by
	// certain tests. Since these things happen in two separate threads, we need to make sure
	// that AsyncServeUrl has returned before we leave this function.
	promise<bool> running;
	auto maybe_running = running.get_future();

	thread_ = thread([this, &running]() {
		auto err = server_.AsyncServeUrl(
			serve_address_,
			[](http::ExpectedIncomingRequestPtr exp_req) {
				if (!exp_req) {
					log::Warning("HttpFileServer: " + exp_req.error().String());
				}
			},
			[this](http::ExpectedIncomingRequestPtr exp_req) { Serve(exp_req); });
		if (err != error::NoError) {
			log::Error("HttpFileServer: " + err.String());
			return;
		}

		running.set_value(true);
		loop_.Run();
	});

	maybe_running.wait();
}

HttpFileServer::~HttpFileServer() {
	loop_.Stop();
	thread_.join();
}

void HttpFileServer::Serve(http::ExpectedIncomingRequestPtr exp_req) {
	if (!exp_req) {
		log::Warning("HttpFileServer: " + exp_req.error().String());
		return;
	}

	auto req = exp_req.value();

	if (req->GetMethod() != http::Method::GET) {
		log::Warning(
			"HttpFileServer: Expected HTTP GET method, but got "
			+ http::MethodToString(req->GetMethod()));
		return;
	}

	auto exp_resp = req->MakeResponse();
	if (!exp_resp) {
		log::Warning("HttpFileServer: " + exp_resp.error().String());
		return;
	}
	auto resp = exp_resp.value();

	auto path = req->GetPath();
	while (path.size() > 0 && path[0] == '/') {
		path = string {path.begin() + 1, path.end()};
	}

	string file_path = path::Join(dir_, path);

	auto exp_stream = io::OpenIfstream(file_path);
	if (!exp_stream) {
		resp->SetStatusCodeAndMessage(http::StatusNotFound, exp_stream.error().String());
		resp->SetHeader("Content-Length", "0");
		resp->SetBodyReader(make_shared<io::StringReader>(""));
	} else {
		auto exp_size = io::FileSize(file_path);
		if (!exp_size) {
			log::Warning("HttpFileServer: " + exp_size.error().String());
			resp->SetStatusCodeAndMessage(
				http::StatusInternalServerError, exp_size.error().String());
			resp->SetHeader("Content-Length", "0");
			return;
		}

		resp->SetStatusCodeAndMessage(http::StatusOK, "");
		resp->SetBodyReader(
			make_shared<io::StreamReader>((make_shared<ifstream>(std::move(exp_stream.value())))));

		resp->SetHeader("Content-Length", to_string(exp_size.value()));
	}

	auto err = resp->AsyncReply([](error::Error err) {
		if (err != error::NoError) {
			log::Warning("HttpFileServer: " + err.String());
		}
	});
	if (err != error::NoError) {
		log::Warning("HttpFileServer: " + err.String());
	}
}

} // namespace testing
} // namespace common
} // namespace mender
