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

#include <common/events_io.hpp>

#include <vector>

namespace mender {
namespace common {
namespace events {
namespace io {

AsyncFileDescriptorReader::AsyncFileDescriptorReader(events::EventLoop &loop, int fd) :
	pipe_(GetAsioIoContext(loop), fd),
	cancelled_(make_shared<bool>(false)) {
}

AsyncFileDescriptorReader::AsyncFileDescriptorReader(events::EventLoop &loop) :
	pipe_(GetAsioIoContext(loop)),
	cancelled_(make_shared<bool>(false)) {
}

error::Error AsyncFileDescriptorReader::Open(const string &path) {
	int fd = open(path.c_str(), O_RDONLY);
	if (fd < 0) {
		int err = errno;
		return error::Error(generic_category().default_error_condition(err), "Cannot open " + path);
	}
	pipe_.assign(fd);
	return error::NoError;
}

error::Error AsyncFileDescriptorReader::AsyncRead(
	vector<uint8_t>::iterator start,
	vector<uint8_t>::iterator end,
	mender::common::io::AsyncIoHandler handler) {
	if (end < start) {
		return error::Error(
			make_error_condition(errc::invalid_argument), "AsyncRead: end cannot precede start");
	}
	if (!handler) {
		return error::Error(
			make_error_condition(errc::invalid_argument), "AsyncRead: handler cannot be nullptr");
	}

	*cancelled_ = false;
	auto cancelled {cancelled_};

	asio::mutable_buffer buf {&start[0], size_t(end - start)};
	pipe_.async_read_some(buf, [cancelled, handler](error_code ec, size_t n) {
		if (*cancelled || ec == make_error_code(asio::error::operation_aborted)) {
			return;
		} else if (ec == make_error_code(asio::error::eof)) {
			// n should always be zero. Handling this properly is possible, but tricky,
			// so just relying on assert for now.
			assert(n == 0);
			handler(0, error::NoError);
		} else if (ec) {
			handler(n, error::Error(ec.default_error_condition(), "AsyncRead failed"));
		} else {
			handler(n, error::NoError);
		}
	});

	return error::NoError;
}

void AsyncFileDescriptorReader::Cancel() {
	pipe_.cancel();
	*cancelled_ = true;
}

AsyncFileDescriptorWriter::AsyncFileDescriptorWriter(events::EventLoop &loop, int fd) :
	pipe_(GetAsioIoContext(loop), fd),
	cancelled_(make_shared<bool>(false)) {
}

AsyncFileDescriptorWriter::AsyncFileDescriptorWriter(events::EventLoop &loop) :
	pipe_(GetAsioIoContext(loop)),
	cancelled_(make_shared<bool>(false)) {
}

error::Error AsyncFileDescriptorWriter::Open(const string &path, Append append) {
	int flags = O_WRONLY | O_CREAT;
	switch (append) {
	case Append::Disabled:
		flags |= O_TRUNC;
		break;
	case Append::Enabled:
		flags |= O_APPEND;
		break;
	}
	int fd = open(path.c_str(), flags, 0644);
	if (fd < 0) {
		int err = errno;
		return error::Error(generic_category().default_error_condition(err), "Cannot open " + path);
	}
	pipe_.assign(fd);
	return error::NoError;
}

error::Error AsyncFileDescriptorWriter::AsyncWrite(
	vector<uint8_t>::const_iterator start,
	vector<uint8_t>::const_iterator end,
	mender::common::io::AsyncIoHandler handler) {
	if (end < start) {
		return error::Error(
			make_error_condition(errc::invalid_argument), "AsyncWrite: end cannot precede start");
	}
	if (!handler) {
		return error::Error(
			make_error_condition(errc::invalid_argument), "AsyncWrite: handler cannot be nullptr");
	}

	*cancelled_ = false;
	auto cancelled {cancelled_};

	asio::const_buffer buf {&start[0], size_t(end - start)};
	pipe_.async_write_some(buf, [cancelled, handler](error_code ec, size_t n) {
		if (*cancelled || ec == make_error_code(asio::error::operation_aborted)) {
			return;
		} else if (ec == make_error_code(asio::error::broken_pipe)) {
			// Let's translate broken_pipe. It's a common error, and we don't want to
			// require the caller to match with Boost ASIO errors.
			handler(n, error::Error(make_error_condition(errc::broken_pipe), "AsyncWrite failed"));
		} else if (ec) {
			handler(n, error::Error(ec.default_error_condition(), "AsyncWrite failed"));
		} else {
			handler(n, error::NoError);
		}
	});

	return error::NoError;
}

void AsyncFileDescriptorWriter::Cancel() {
	pipe_.cancel();
	*cancelled_ = true;
}

} // namespace io
} // namespace events
} // namespace common
} // namespace mender
