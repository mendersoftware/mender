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

#include <memory>
#include <system_error>
#include <vector>
#include <istream>
#include <sstream>
#include <string>

namespace mender {
namespace common {
namespace io {

using namespace std;

using mender::common::error::Error;
using mender::common::error::NoError;
using mender::common::expected::Expected;

using ExpectedSize = Expected<size_t, Error>;

class Reader {
public:
	virtual ~Reader() {};

	virtual ExpectedSize Read(vector<uint8_t> &dst) = 0;
};

class Writer {
public:
	virtual ~Writer() {};

	virtual ExpectedSize Write(const vector<uint8_t> &dst) = 0;
};

class ReadWriter : virtual public Reader, virtual public Writer {};

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
	std::istream &is_;

public:
	StreamReader(std::istream &stream) :
		is_ {stream} {
	}
	StreamReader(std::istream &&stream) :
		is_ {stream} {
	}
	ExpectedSize Read(vector<uint8_t> &dst) override {
		is_.read(reinterpret_cast<char *>(&dst[0]), dst.size());
		return is_.gcount();
	}
};

/* Discards all data written to it */
class Discard : virtual public Writer {
	ExpectedSize Write(const vector<uint8_t> &dst) override {
		return dst.size();
	}
};

class StringReader : virtual public Reader {
private:
	StreamReader reader_;
	std::stringstream s_;

public:
	StringReader(string &str) :
		s_ {str},
		reader_ {s_} {
	}
	StringReader(string &&str) :
		s_ {str},
		reader_ {s_} {
	}

	ExpectedSize Read(vector<uint8_t> &dst) override {
		return reader_.Read(dst);
	}
};

} // namespace io
} // namespace common
} // namespace mender

#endif // MENDER_COMMON_IO_HPP
