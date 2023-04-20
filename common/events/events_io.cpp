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

AsyncReaderFromReader::AsyncReaderFromReader(
	EventLoop &loop, mender::common::io::ReaderPtr reader) :
	cancelled_(make_shared<atomic<bool>>(false)),
	reader_(reader),
	loop_(loop) {
}

AsyncReaderFromReader::~AsyncReaderFromReader() {
	Cancel();
}

error::Error AsyncReaderFromReader::AsyncRead(
	vector<uint8_t>::iterator start,
	vector<uint8_t>::iterator end,
	mender::common::io::AsyncIoHandler handler) {
	if (reader_thread_.joinable()) {
		// Clean up previous operation. Careful, this can hang if AsyncRead is called twice
		// in a row without letting the handler execute.
		reader_thread_.join();
	}

	auto cancelled = cancelled_;
	// Expensive to create a thread on every read. Future optimization: Create a thread which
	// receives work through some channel.
	reader_thread_ = thread([this, start, end, handler, cancelled]() {
		auto result = reader_->Read(start, end);
		loop_.Post([result, handler, cancelled]() {
			if (!*cancelled) {
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
		// Need to wait for thread to finish because iterators may be destroyed after this
		// function has returned.
		reader_thread_.join();
	}
}

AsyncWriterFromWriter::AsyncWriterFromWriter(
	EventLoop &loop, mender::common::io::WriterPtr writer) :
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
	mender::common::io::AsyncIoHandler handler) {
	if (writer_thread_.joinable()) {
		// Clean up previous operation. Careful, this can hang if AsyncWrite is called twice
		// in a row without letting the handler execute.
		writer_thread_.join();
	}

	auto cancelled = cancelled_;
	// Expensive to create a thread on every write. Future optimization: Create a thread which
	// receives work through some channel.
	writer_thread_ = thread([this, start, end, handler, cancelled]() {
		auto result = writer_->Write(start, end);
		loop_.Post([result, handler, cancelled]() {
			if (!*cancelled) {
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
		// Need to wait for thread to finish because iterators may be destroyed after this
		// function has returned.
		writer_thread_.join();
	}
}

} // namespace io
} // namespace events
} // namespace common
} // namespace mender
