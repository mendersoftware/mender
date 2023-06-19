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

#include <common/io.hpp>

#include <config.h>

#include <cerrno>
#include <cstdint>
#include <cstring>
#include <istream>
#include <memory>
#include <streambuf>
#include <vector>
#include <fstream>

namespace mender {
namespace common {
namespace io {

namespace error = mender::common::error;
namespace expected = mender::common::expected;

Error Copy(Writer &dst, Reader &src) {
	vector<uint8_t> buffer(MENDER_BUFSIZE);
	return Copy(dst, src, buffer);
}

Error Copy(Writer &dst, Reader &src, vector<uint8_t> &buffer) {
	while (true) {
		auto r_result = src.Read(buffer.begin(), buffer.end());
		if (!r_result) {
			return r_result.error();
		} else if (r_result.value() == 0) {
			return NoError;
		} else if (r_result.value() > buffer.size()) {
			return error::MakeError(
				error::ProgrammingError,
				"Read returned more bytes than requested. This is a bug in the Read function.");
		}

		auto w_result = dst.Write(buffer.cbegin(), buffer.cbegin() + r_result.value());
		if (!w_result) {
			return w_result.error();
		} else if (w_result.value() == 0) {
			// Should this even happen?
			return Error(std::error_condition(std::errc::io_error), "Zero write when copying data");
		} else if (r_result.value() != w_result.value()) {
			return Error(
				std::error_condition(std::errc::io_error), "Short write when copying data");
		}
	}
}

void ByteWriter::SetUnlimited(bool enabled) {
	unlimited_ = enabled;
}

ExpectedSize ByteWriter::Write(
	vector<uint8_t>::const_iterator start, vector<uint8_t>::const_iterator end) {
	assert(end > start);
	Vsize max_write {receiver_->size() - bytes_written_};
	if (max_write == 0 && !unlimited_) {
		return expected::unexpected(Error(make_error_condition(errc::no_space_on_device), ""));
	}
	Vsize iterator_size {static_cast<Vsize>(end - start)};
	Vsize bytes_to_write;
	if (unlimited_) {
		bytes_to_write = iterator_size;
		if (max_write < bytes_to_write) {
			receiver_->resize(bytes_written_ + bytes_to_write);
			max_write = bytes_to_write;
		}
	} else {
		bytes_to_write = min(iterator_size, max_write);
	}
	auto it = next(receiver_->begin(), bytes_written_);
	std::copy_n(start, bytes_to_write, it);
	bytes_written_ += bytes_to_write;
	return bytes_to_write;
}


ExpectedSize StreamWriter::Write(
	vector<uint8_t>::const_iterator start, vector<uint8_t>::const_iterator end) {
	os_->write(reinterpret_cast<const char *>(&*start), end - start);
	if (!(*(os_.get()))) {
		return expected::unexpected(Error(make_error_condition(errc::io_error), ""));
	}
	return end - start;
}

class ReaderStreamBuffer : public streambuf {
public:
	ReaderStreamBuffer(Reader &reader) :
		reader_ {reader},
		buf_(buf_size_) {};
	streambuf::int_type underflow() override;

private:
	static const Vsize buf_size_ = MENDER_BUFSIZE;
	Reader &reader_;
	vector<uint8_t> buf_;
};

streambuf::int_type ReaderStreamBuffer::underflow() {
	// eback -- pointer to the first char (byte)
	// gptr  -- pointer to the current char (byte)
	// egptr -- pointer past the last char (byte)

	// This function is only called if gptr() == nullptr or gptr() >= egptr(),
	// i.e. if there's nothing more to read.
	if (this->gptr() >= this->egptr()) {
		errno = 0;
		auto ex_n_read = reader_.Read(buf_.begin(), buf_.end());
		streamsize n_read;
		if (ex_n_read) {
			n_read = ex_n_read.value();
		} else {
			// There is no way to return an error from underflow(), generally
			// the streams only care about how much data was read. No data or
			// less data then requested by the caller of istream.read() means
			// eofbit and failbit are set. If the user code wants to get the
			// error or check if there was an error, it needs to check errno.
			//
			// So as long as we don't clear errno after a failure in the
			// reader_.Read() above, error handling works as usual and returning
			// eof below is all that needs to happen here.
			//
			// In case errno is not set for some reason, let's try to get it
			// from the error with a fallback to a generic I/O error.
			if (errno == 0) {
				if (ex_n_read.error().code.category() == generic_category()) {
					errno = ex_n_read.error().code.value();
				} else {
					errno = EIO;
				}
			}
			n_read = 0;
		}

		streambuf::char_type *first = reinterpret_cast<streambuf::char_type *>(buf_.data());

		// set eback, gptr, egptr
		this->setg(first, first, first + n_read);
	}

	return this->gptr() == this->egptr() ? std::char_traits<char>::eof()
										 : std::char_traits<char>::to_int_type(*this->gptr());
};

/**
 * A variant of the #istream class that takes ownership of the #streambuf buffer
 * created for it.
 *
 * @note Base #istream is designed to work on shared buffers so it doesn't
 *       destruct/delete the buffer.
 */
class istreamWithUniqueBuffer : public istream {
public:
	// The unique_ptr, &&buf and std::move() model this really nicely -- a
	// unique_ptr rvalue (i.e. temporary) is required and it's moved into the
	// object. The default destructor then takes care of cleaning up properly.
	istreamWithUniqueBuffer(unique_ptr<streambuf> &&buf) :
		istream(buf.get()),
		buf_ {std::move(buf)} {};

private:
	unique_ptr<streambuf> buf_;
};

unique_ptr<istream> Reader::GetStream() {
	return unique_ptr<istream>(
		new istreamWithUniqueBuffer(unique_ptr<ReaderStreamBuffer>(new ReaderStreamBuffer(*this))));
};

ExpectedIfstream OpenIfstream(const string &path) {
	ifstream is;
	errno = 0;
	is.open(path);
	if (!is) {
		int io_errno = errno;
		return ExpectedIfstream(expected::unexpected(error::Error(
			generic_category().default_error_condition(io_errno),
			"Failed to open '" + path + "' for reading")));
	}
	return ExpectedIfstream(move(is));
}

ExpectedOfstream OpenOfstream(const string &path) {
	ofstream os;
	errno = 0;
	os.open(path);
	if (!os) {
		int io_errno = errno;
		return ExpectedOfstream(expected::unexpected(error::Error(
			generic_category().default_error_condition(io_errno),
			"Failed to open '" + path + "' for writing")));
	}
	return ExpectedOfstream(move(os));
}

error::Error WriteStringIntoOfstream(ofstream &os, const string &data) {
	errno = 0;
	os.write(data.data(), data.size());
	if (os.bad() || os.fail()) {
		int io_errno = errno;
		return error::Error(
			std::generic_category().default_error_condition(io_errno),
			"Failed to write data into the stream");
	}

	return error::NoError;
}

ExpectedSize StreamReader::Read(vector<uint8_t>::iterator start, vector<uint8_t>::iterator end) {
	is_->read(reinterpret_cast<char *>(&*start), end - start);
	if (is_->bad()) {
		int io_error = errno;
		return expected::unexpected(
			Error(std::generic_category().default_error_condition(io_error), ""));
	}
	return is_->gcount();
}

expected::ExpectedSize FileSize(const string &path) {
	// Probably not as efficient as stat(), but portable.
	ifstream is(path, ifstream::ate | ifstream::binary);
	if (is.bad()) {
		int io_error = errno;
		return expected::unexpected(
			Error(std::generic_category().default_error_condition(io_error), "FileSize"));
	}
	return is.tellg();
}

} // namespace io
} // namespace common
} // namespace mender
