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

namespace paths {
extern const string Stdin;
}

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

enum class Repeat {
	Yes,
	No,
};

using AsyncIoHandler = function<void(ExpectedSize)>;
using RepeatedAsyncIoHandler = function<Repeat(ExpectedSize)>;

class AsyncReader : virtual public Canceller {
public:
	// Note: iterators generally need to remain valid until either the handler or `Cancel()` is
	// called.
	virtual error::Error AsyncRead(
		vector<uint8_t>::iterator start, vector<uint8_t>::iterator end, AsyncIoHandler handler) = 0;

	// Calls AsyncRead repeatedly with the same iterators and handler, until the stream is
	// exhausted or an error occurs. All errors will be returned through the handler, even
	// initial errors from AsyncRead.
	void RepeatedAsyncRead(
		vector<uint8_t>::iterator start,
		vector<uint8_t>::iterator end,
		RepeatedAsyncIoHandler handler);
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
protected:
	shared_ptr<std::istream> is_;

public:
	StreamReader(std::istream &stream) :
		// For references, initialize a shared_ptr with a null deleter, since we don't own
		// the object.
		is_ {&stream, [](std::istream *stream) {}} {
	}
	StreamReader(shared_ptr<std::istream> stream) :
		is_ {stream} {
	}
	ExpectedSize Read(vector<uint8_t>::iterator start, vector<uint8_t>::iterator end) override;
};

using ExpectedIfstream = expected::expected<ifstream, error::Error>;
using ExpectedSharedIfstream = expected::expected<shared_ptr<ifstream>, error::Error>;
using ExpectedOfstream = expected::expected<ofstream, error::Error>;
using ExpectedSharedOfstream = expected::expected<shared_ptr<ofstream>, error::Error>;
ExpectedIfstream OpenIfstream(const string &path);
ExpectedSharedIfstream OpenSharedIfstream(const string &path);
ExpectedOfstream OpenOfstream(const string &path, bool append = false);
ExpectedSharedOfstream OpenSharedOfstream(const string &path, bool append = false);

class FileReader : virtual public StreamReader {
public:
	FileReader(const string &path) :
		StreamReader(shared_ptr<std::istream>()),
		path_ {path} {};

	ExpectedSize Read(vector<uint8_t>::iterator start, vector<uint8_t>::iterator end) override {
		// We cannot open the stream in the constructor because it can fail and
		// there's no way to report that error. However, the check below is
		// cheap compared to the I/O that happens in this function, so a very
		// little overhead.
		if (!is_) {
			auto ex_is = OpenSharedIfstream(path_);
			if (!ex_is) {
				return expected::unexpected(ex_is.error());
			}
			is_ = ex_is.value();
		}
		return StreamReader::Read(start, end);
	}

	error::Error Rewind();

private:
	string path_;
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
		s_ {std::move(sr.s_)},
		reader_ {new StreamReader(s_)} {
	}
	StringReader &operator=(StringReader &&sr) {
		s_ = std::move(sr.s_);
		reader_.reset(new StreamReader(s_));
		return *this;
	}

	ExpectedSize Read(vector<uint8_t>::iterator start, vector<uint8_t>::iterator end) override {
		return reader_->Read(start, end);
	}
};

using Vsize = vector<uint8_t>::size_type;

class ByteReader : virtual public Reader {
private:
	shared_ptr<vector<uint8_t>> emitter_;
	Vsize bytes_read_ {0};

public:
	ByteReader(vector<uint8_t> &emitter) :
		emitter_ {&emitter, [](vector<uint8_t> *vec) {}} {
	}

	ByteReader(shared_ptr<vector<uint8_t>> emitter) :
		emitter_ {emitter} {
	}

	ExpectedSize Read(vector<uint8_t>::iterator start, vector<uint8_t>::iterator end) override;
};

class ByteWriter : virtual public Writer {
protected:
	Vsize bytes_written_ {0};

private:
	shared_ptr<vector<uint8_t>> receiver_;
	bool unlimited_ {false};

public:
	ByteWriter(vector<uint8_t> &receiver) :
		receiver_ {&receiver, [](vector<uint8_t> *vec) {}} {
	}

	ByteWriter(shared_ptr<vector<uint8_t>> receiver) :
		receiver_ {receiver} {
	}

	// Will extend the vector if necessary. Probably a bad idea in production code, but useful
	// in tests.
	void SetUnlimited(bool enabled);

	ExpectedSize Write(
		vector<uint8_t>::const_iterator start, vector<uint8_t>::const_iterator end) override;
};

class ByteOffsetWriter : public ByteWriter {
public:
	ByteOffsetWriter(vector<uint8_t> &receiver, Vsize offset) :
		ByteWriter(receiver) {
		bytes_written_ = offset;
	}

	ByteOffsetWriter(shared_ptr<vector<uint8_t>> receiver, Vsize offset) :
		ByteWriter(receiver) {
		bytes_written_ = offset;
	}
};

class StreamWriter : virtual public Writer {
private:
	shared_ptr<std::ostream> os_;

public:
	StreamWriter(std::ostream &stream) :
		os_ {&stream, [](std::ostream *str) {}} {
	}
	StreamWriter(shared_ptr<std::ostream> stream) :
		os_ {stream} {
	}
	ExpectedSize Write(
		vector<uint8_t>::const_iterator start, vector<uint8_t>::const_iterator end) override;
};

error::Error WriteStringIntoOfstream(ofstream &os, const string &data);

expected::ExpectedSize FileSize(const string &path);

} // namespace io
} // namespace common
} // namespace mender

#endif // MENDER_COMMON_IO_HPP
