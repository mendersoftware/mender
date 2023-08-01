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
	reader_ {reader},
	loop_ {loop} {
}

AsyncReaderFromReader::~AsyncReaderFromReader() {
	Cancel();
}

error::Error AsyncReaderFromReader::AsyncRead(
	vector<uint8_t>::iterator start, vector<uint8_t>::iterator end, mio::AsyncIoHandler handler) {
	// Simple, "cheating" implementation, we just do it synchronously.
	in_progress_ = true;
	loop_.Post([this, start, end, handler]() {
		auto result = reader_->Read(start, end);
		in_progress_ = false;
		handler(result);
	});

	return error::NoError;
}

void AsyncReaderFromReader::Cancel() {
	// Cancel() is not allowed on normal Readers.
	assert(!in_progress_);
}

AsyncWriterFromWriter::AsyncWriterFromWriter(EventLoop &loop, mio::WriterPtr writer) :
	writer_ {writer},
	loop_ {loop} {
}

AsyncWriterFromWriter::~AsyncWriterFromWriter() {
	Cancel();
}

error::Error AsyncWriterFromWriter::AsyncWrite(
	vector<uint8_t>::const_iterator start,
	vector<uint8_t>::const_iterator end,
	mio::AsyncIoHandler handler) {
	// Simple, "cheating" implementation, we just do it synchronously.
	in_progress_ = true;
	loop_.Post([this, start, end, handler]() {
		auto result = writer_->Write(start, end);
		in_progress_ = false;
		handler(result);
	});

	return error::NoError;
}

void AsyncWriterFromWriter::Cancel() {
	// Cancel() is not allowed on normal Writers.
	assert(!in_progress_);
}

ReaderFromAsyncReader::ReaderFromAsyncReader() {
}

mio::ExpectedReaderPtr ReaderFromAsyncReader::Construct(AsyncReaderFromEventLoopFunc func) {
	shared_ptr<ReaderFromAsyncReader> reader(new ReaderFromAsyncReader);
	auto async_reader = func(reader->event_loop_);
	if (!async_reader) {
		return expected::unexpected(async_reader.error());
	}

	reader->reader_ = async_reader.value();
	return reader;
}

mio::ExpectedSize ReaderFromAsyncReader::Read(
	vector<uint8_t>::iterator start, vector<uint8_t>::iterator end) {
	mio::ExpectedSize read;
	error::Error err;
	err = reader_->AsyncRead(start, end, [this, &read](mio::ExpectedSize num_read) {
		read = num_read;
		event_loop_.Stop();
	});
	if (err != error::NoError) {
		return expected::unexpected(err);
	}

	event_loop_.Run();

	return read;
}

} // namespace io
} // namespace events
} // namespace common
} // namespace mender
