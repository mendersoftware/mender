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
	bool finished = false;
	event_loop_.Post([start, end, this, &finished, &read]() {
		auto err =
			reader_->AsyncRead(start, end, [this, &finished, &read](mio::ExpectedSize num_read) {
				read = num_read;
				finished = true;
				event_loop_.Stop();
			});
		if (err != error::NoError) {
			read = expected::unexpected(err);
			finished = true;
			event_loop_.Stop();
		}
	});

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

TeeReader::TeeReaderLeafPtr TeeReader::MakeAsyncReader() {
	auto reader = make_shared<TeeReaderLeaf>(shared_from_this());
	leaf_readers_.insert({reader, TeeReaderLeafContext {}});
	auto bytes_missing = source_reader_->Rewind();
	leaf_readers_[reader].buffer_bytes_missing = bytes_missing;
	return reader;
}

error::Error TeeReader::ReadyToAsyncRead(
	TeeReader::TeeReaderLeafPtr leaf_reader,
	vector<uint8_t>::iterator start,
	vector<uint8_t>::iterator end,
	mio::AsyncIoHandler handler) {
	// The reader must exist in the internal map.
	auto found = leaf_readers_.find(leaf_reader);
	AssertOrReturnError(found != leaf_readers_.end());

	if (leaf_readers_[leaf_reader].buffer_bytes_missing > 0) {
		// Special case, reading missing bytes
		TeeReaderLeafContext &ctx = leaf_readers_[leaf_reader];
		auto to_read = std::min(ctx.buffer_bytes_missing, (size_t) (end - start));
		auto handler_wrapper = [handler, &ctx](mio::ExpectedSize result) {
			if (result) {
				ctx.buffer_bytes_missing -= result.value();
			}
			handler(result);
		};

		auto err = source_reader_->AsyncRead(start, start + to_read, handler_wrapper);
		if (err != error::NoError) {
			handler(expected::unexpected(err));
		}
	} else {
		leaf_readers_[leaf_reader].pending_read.start = start;
		leaf_readers_[leaf_reader].pending_read.end = end;
		leaf_readers_[leaf_reader].pending_read.handler = handler;
		if (++ready_to_read == leaf_readers_.size()) {
			DoAsyncRead();
			ready_to_read = 0;
		}
	}

	return error::NoError;
}

void TeeReader::CallAllHandlers(mio::ExpectedSize result) {
	// Makes a copy of the handlers and then calls them sequentially
	vector<mio::AsyncIoHandler> handlers;
	for (const auto &it : leaf_readers_) {
		handlers.push_back(it.second.pending_read.handler);
	}
	for (const auto &h : handlers) {
		h(result);
	}
}

void TeeReader::DoAsyncRead() {
	auto handler = [this](mio::ExpectedSize result) {
		if (!result) {
			CallAllHandlers(result);
			return;
		};

		auto start_iterator = leaf_readers_.begin()->second.pending_read.start;
		auto read_bytes = result.value();
		for_each(
			std::next(leaf_readers_.begin()),
			leaf_readers_.end(),
			[start_iterator,
			 read_bytes](const std::pair<TeeReaderLeafPtr, TeeReaderLeafContext> r) {
				std::copy_n(start_iterator, read_bytes, r.second.pending_read.start);
			});

		CallAllHandlers(result);
	};

	auto min_read = std::min_element(
		leaf_readers_.cbegin(),
		leaf_readers_.cend(),
		[](const std::pair<TeeReaderLeafPtr, TeeReaderLeafContext> r1,
		   std::pair<TeeReaderLeafPtr, TeeReaderLeafContext> r2) {
			return (r1.second.pending_read.end - r1.second.pending_read.start)
				   < (r2.second.pending_read.end - r2.second.pending_read.start);
		});
	auto bytes_to_read = min_read->second.pending_read.end - min_read->second.pending_read.start;

	auto err = source_reader_->AsyncRead(
		leaf_readers_.begin()->second.pending_read.start,
		leaf_readers_.begin()->second.pending_read.start + bytes_to_read,
		handler);
	if (err != error::NoError) {
		CallAllHandlers(expected::unexpected(err));
	}
}

error::Error TeeReader::TeeReaderLeaf::AsyncRead(
	vector<uint8_t>::iterator start, vector<uint8_t>::iterator end, mio::AsyncIoHandler handler) {
	return tee_reader_->ReadyToAsyncRead(shared_from_this(), start, end, handler);
}

} // namespace io
} // namespace events
} // namespace common
} // namespace mender
