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
	cancelled_ = make_shared<bool>(false);
	auto &cancelled = cancelled_;
	loop_.Post([this, cancelled, start, end, handler]() {
		if (!*cancelled) {
			in_progress_ = true;
			// Simple, "cheating" implementation, we just do it synchronously.
			auto result = reader_->Read(start, end);
			in_progress_ = false;
			handler(result);
		}
	});

	return error::NoError;
}

void AsyncReaderFromReader::Cancel() {
	// Cancel() is not allowed on normal Readers.
	assert(!in_progress_);
	if (cancelled_) {
		*cancelled_ = true;
		cancelled_.reset();
	}
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
	cancelled_ = make_shared<bool>(false);
	auto &cancelled = cancelled_;
	loop_.Post([this, cancelled, start, end, handler]() {
		if (!*cancelled) {
			in_progress_ = true;
			// Simple, "cheating" implementation, we just do it synchronously.
			auto result = writer_->Write(start, end);
			in_progress_ = false;
			handler(result);
		}
	});

	return error::NoError;
}

void AsyncWriterFromWriter::Cancel() {
	// Cancel() is not allowed on normal Writers.
	assert(!in_progress_);
	if (cancelled_) {
		*cancelled_ = true;
		cancelled_.reset();
	}
}

ReaderFromAsyncReader::ReaderFromAsyncReader(EventLoop &event_loop, mio::AsyncReaderPtr reader) :
	event_loop_(event_loop),
	reader_(reader) {
}

ReaderFromAsyncReader::ReaderFromAsyncReader(EventLoop &event_loop, mio::AsyncReader &reader) :
	event_loop_(event_loop),
	// For references, just use a destructor-less pointer.
	reader_(&reader, [](mio::AsyncReader *) {}) {
}

mio::ExpectedSize ReaderFromAsyncReader::Read(
	vector<uint8_t>::iterator start, vector<uint8_t>::iterator end) {
	mio::ExpectedSize read;
	error::Error err;
	bool finished = false;
	err = reader_->AsyncRead(start, end, [this, &finished, &read](mio::ExpectedSize num_read) {
		read = num_read;
		finished = true;
		event_loop_.Stop();
	});
	if (err != error::NoError) {
		return expected::unexpected(err);
	}

	// Since the same event loop may have been used to call into this function, run the event
	// loop recursively to keep processing events.
	event_loop_.Run();

	if (!finished) {
		// If this happens then it means that the event loop was stopped by somebody
		// else. We have no choice now but to return error, since we have to get out of this
		// stack frame. We also need to re-stop the event loop, since the first stop was
		// spent on getting here.
		event_loop_.Stop();
		return expected::unexpected(
			error::Error(make_error_condition(errc::operation_canceled), "Event loop was stopped"));
	}

	return read;
}

} // namespace io
} // namespace events
} // namespace common
} // namespace mender
