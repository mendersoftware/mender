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

#ifndef MENDER_COMMON_IO_HPP
#define MENDER_COMMON_IO_HPP

#include <common/error.hpp>
#include <common/expected.hpp>

#include <cstdint>
#include <iterator>
#include <memory>
#include <system_error>
#include <vector>
#include <istream>
#include <sstream>
#include <string>
#include <algorithm>
#include <ostream>
#include <iostream>
#include <fstream>

namespace mender {
namespace common {
namespace io {

using namespace std;

namespace expected = mender::common::expected;

using mender::common::error::Error;
using mender::common::error::NoError;

using mender::common::expected::ExpectedSize;

class Reader {
public:
	virtual ~Reader() {};

	virtual ExpectedSize Read(vector<uint8_t>::iterator start, vector<uint8_t>::iterator end) = 0;

	unique_ptr<istream> GetStream();
};
using ReaderPtr = shared_ptr<Reader>;
using ExpectedReaderPtr = expected::expected<ReaderPtr, Error>;

class Writer {
public:
	virtual ~Writer() {};

	virtual ExpectedSize Write(
		vector<uint8_t>::const_iterator start, vector<uint8_t>::const_iterator end) = 0;
};
using WriterPtr = shared_ptr<Writer>;
using ExpectedWriterPtr = expected::expected<WriterPtr, Error>;

class ReadWriter : virtual public Reader, virtual public Writer {};
using ReadWriterPtr = shared_ptr<ReadWriter>;
using ExpectedReadWriterPtr = expected::expected<ReadWriterPtr, Error>;

class Canceller {
public:
	virtual ~Canceller() {
	}

	virtual void Cancel() = 0;
};

using AsyncIoHandler = function<void(size_t n, error::Error err)>;

class AsyncReader : virtual public Canceller {
public:
	// Note: iterators generally need to remain valid until either the handler or `Cancel()` is
	// called.
	virtual error::Error AsyncRead(
		vector<uint8_t>::iterator start, vector<uint8_t>::iterator end, AsyncIoHandler handler) = 0;
};
using AsyncReaderPtr = shared_ptr<AsyncReader>;

class AsyncWriter : virtual public Canceller {
public:
	// Note: iterators generally need to remain valid until either the handler or `Cancel()` is
	// called.
	virtual error::Error AsyncWrite(
		vector<uint8_t>::const_iterator start,
		vector<uint8_t>::const_iterator end,
		AsyncIoHandler handler) = 0;
};
using AsyncWriterPtr = shared_ptr<AsyncWriter>;

class AsyncReadWriter : virtual public AsyncReader, virtual public AsyncWriter {};
using AsyncReadWriterPtr = shared_ptr<AsyncReadWriter>;

using ExpectedAsyncReaderPtr = expected::expected<AsyncReaderPtr, error::Error>;
using ExpectedAsyncWriterPtr = expected::expected<AsyncWriterPtr, error::Error>;
using ExpectedAsyncReadWriterPtr = expected::expected<AsyncReadWriterPtr, error::Error>;

/**
 * Stream the data from `src` to `dst` until encountering EOF or an error.
 */
Error Copy(Writer &dst, Reader &src);

/**
 * Stream the data from `src` to `dst` until encountering EOF or an error, using `buffer` as an
 * intermediate. The block size will be the size of `buffer`.
 */
Error Copy(Writer &dst, Reader &src, vector<uint8_t> &buffer);

class StreamReader : virtual public Reader {
private:
	shared_ptr<std::istream> is_;

public:
	StreamReader(std::istream &stream) :
		// For references, initialize a shared_ptr with a null deleter, since we don't own
		// the object.
		is_(&stream, [](std::istream *stream) {}) {
	}
	StreamReader(shared_ptr<std::istream> &stream) :
		is_ {stream} {
	}
	ExpectedSize Read(vector<uint8_t>::iterator start, vector<uint8_t>::iterator end) override {
		is_->read(reinterpret_cast<char *>(&*start), end - start);
		if (is_->bad()) {
			int io_error = errno;
			return expected::unexpected(
				Error(std::generic_category().default_error_condition(io_error), ""));
		}
		return is_->gcount();
	}
};

/* Discards all data written to it */
class Discard : virtual public Writer {
	ExpectedSize Write(
		vector<uint8_t>::const_iterator start, vector<uint8_t>::const_iterator end) override {
		return end - start;
	}
};

class StringReader : virtual public Reader {
private:
	std::stringstream s_;
	unique_ptr<StreamReader> reader_;

public:
	StringReader(const string &str) :
		s_ {str},
		reader_ {new StreamReader(s_)} {
	}
	StringReader(string &&str) :
		s_ {str},
		reader_ {new StreamReader(s_)} {
	}
	StringReader(StringReader &&sr) :
		s_ {move(sr.s_)},
		reader_ {new StreamReader(s_)} {
	}
	StringReader &operator=(StringReader &&sr) {
		s_ = move(sr.s_);
		reader_.reset(new StreamReader(s_));
		return *this;
	}

	ExpectedSize Read(vector<uint8_t>::iterator start, vector<uint8_t>::iterator end) override {
		return reader_->Read(start, end);
	}
};

using Vsize = vector<uint8_t>::size_type;

class ByteWriter : virtual public Writer {
private:
	shared_ptr<vector<uint8_t>> receiver_;
	Vsize bytes_written_ {0};
	bool unlimited_ {false};

public:
	ByteWriter(vector<uint8_t> &receiver) :
		receiver_(&receiver, [](vector<uint8_t> *vec) {}) {
	}

	ByteWriter(shared_ptr<vector<uint8_t>> &receiver) :
		receiver_ {receiver} {
	}

	// Will extend the vector if necessary. Probably a bad idea in production code, but useful
	// in tests.
	void SetUnlimited(bool enabled);

	ExpectedSize Write(
		vector<uint8_t>::const_iterator start, vector<uint8_t>::const_iterator end) override;
};

class StreamWriter : virtual public Writer {
private:
	shared_ptr<std::ostream> os_;

public:
	StreamWriter(std::ostream &stream) :
		os_(&stream, [](std::ostream *str) {}) {
	}
	StreamWriter(shared_ptr<std::ostream> &stream) :
		os_ {stream} {
	}
	ExpectedSize Write(
		vector<uint8_t>::const_iterator start, vector<uint8_t>::const_iterator end) override;
};

using ExpectedIfstream = expected::expected<ifstream, error::Error>;
using ExpectedOfstream = expected::expected<ofstream, error::Error>;
ExpectedIfstream OpenIfstream(const string &path);
ExpectedOfstream OpenOfstream(const string &path);

error::Error WriteStringIntoOfstream(ofstream &os, const string &data);

} // namespace io
} // namespace common
} // namespace mender

#endif // MENDER_COMMON_IO_HPP
