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

namespace mender {
namespace common {
namespace events {
namespace io {

AsyncReaderFromReader::AsyncReaderFromReader(EventLoop &loop, mio::ReaderPtr reader) :
	cancelled_(make_shared<atomic<bool>>(false)),
	reader_(reader),
	loop_(loop) {
}

AsyncReaderFromReader::~AsyncReaderFromReader() {
	Cancel();
}

error::Error AsyncReaderFromReader::AsyncRead(
	vector<uint8_t>::iterator start, vector<uint8_t>::iterator end, mio::AsyncIoHandler handler) {
	if (reader_thread_.joinable()) {
		return error::Error(
			make_error_condition(errc::operation_in_progress), "AsyncRead already in progress");
	}

	auto cancelled = cancelled_;
	// Expensive to create a thread on every read. Future optimization: Create a thread which
	// receives work through some channel.
	reader_thread_ = thread([this, start, end, handler, cancelled]() {
		auto result = reader_->Read(start, end);
		loop_.Post([this, result, handler, cancelled]() {
			if (!*cancelled) {
				// This should always be true, but let's use this as a safeguard
				// anyway.
				assert(reader_thread_.joinable());
				if (reader_thread_.joinable()) {
					reader_thread_.join();
				}
				if (result) {
					handler(result.value(), error::NoError);
				} else {
					handler(0, result.error());
				}
			}
		});
	});

	return error::NoError;
}

void AsyncReaderFromReader::Cancel() {
	*cancelled_ = true;
	if (reader_thread_.joinable()) {
		// Note: Need to wait for thread to finish because iterators may be destroyed after
		// this function has returned.
		reader_thread_.join();
	}
}

AsyncWriterFromWriter::AsyncWriterFromWriter(EventLoop &loop, mio::WriterPtr writer) :
	cancelled_(make_shared<atomic<bool>>(false)),
	writer_(writer),
	loop_(loop) {
}

AsyncWriterFromWriter::~AsyncWriterFromWriter() {
	Cancel();
}

error::Error AsyncWriterFromWriter::AsyncWrite(
	vector<uint8_t>::const_iterator start,
	vector<uint8_t>::const_iterator end,
	mio::AsyncIoHandler handler) {
	if (writer_thread_.joinable()) {
		return error::Error(
			make_error_condition(errc::operation_in_progress), "AsyncWrite already in progress");
	}

	auto cancelled = cancelled_;
	// Expensive to create a thread on every write. Future optimization: Create a thread which
	// receives work through some channel.
	writer_thread_ = thread([this, start, end, handler, cancelled]() {
		auto result = writer_->Write(start, end);
		loop_.Post([this, result, handler, cancelled]() {
			if (!*cancelled) {
				// This should always be true, but let's use this as a safeguard
				// anyway.
				assert(writer_thread_.joinable());
				if (writer_thread_.joinable()) {
					writer_thread_.join();
				}
				if (result) {
					handler(result.value(), error::NoError);
				} else {
					handler(0, result.error());
				}
			}
		});
	});

	return error::NoError;
}

void AsyncWriterFromWriter::Cancel() {
	*cancelled_ = true;
	if (writer_thread_.joinable()) {
		// Note: Need to wait for thread to finish because iterators may be destroyed after
		// this function has returned.
		writer_thread_.join();
	}
}

ReaderFromAsyncReader::ReaderFromAsyncReader(AsyncReaderFromEventLoopFunc func) :
	reader_(func(event_loop_)) {
}

mio::ExpectedSize ReaderFromAsyncReader::Read(
	vector<uint8_t>::iterator start, vector<uint8_t>::iterator end) {
	size_t read;
	error::Error err;
	error::Error inner_err;
	err = reader_->AsyncRead(start, end, [this, &read, &inner_err](size_t n, error::Error err) {
		read = n;
		inner_err = err;
		event_loop_.Stop();
	});
	if (err != error::NoError) {
		return expected::unexpected(err);
	}

	event_loop_.Run();

	if (inner_err != error::NoError) {
		return expected::unexpected(inner_err);
	}
	return read;
}

} // namespace io
} // namespace events
} // namespace common
} // namespace mender
