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

#ifndef MENDER_COMMON_TAR_HPP
#define MENDER_COMMON_TAR_HPP

#include <config.h>

#include <string>
#include <vector>

#include <common/io.hpp>
#include <common/expected.hpp>
#include <common/error.hpp>

#include <artifact/tar/tar_errors.hpp>

#ifdef MENDER_TAR_LIBARCHIVE
#include <libarchive/wrapper.hpp>
#endif

namespace mender {
namespace tar {

using namespace std;

namespace expected = mender::common::expected;
namespace error = mender::common::error;
namespace io = mender::common::io;

using Error = error::Error;
using ExpectedSize = expected::ExpectedSize;

class Entry : public io::Reader {
private:
	string name_;
	size_t total_size_;

	Reader &reader_;

	// Reader data
	size_t nr_bytes_read_ {0};

public:
	Entry(const string &name, size_t archive_size, Reader &reader) :
		name_ {name},
		total_size_ {archive_size},
		reader_ {reader} {
	}

	string Name() {
		return name_;
	}

	size_t Size() {
		return total_size_;
	}

	ExpectedSize Read(vector<uint8_t>::iterator start, vector<uint8_t>::iterator end) override;
};

using ExpectedEntry = expected::expected<Entry, error::Error>;

class Reader : io::Reader {
private:
#ifdef MENDER_TAR_LIBARCHIVE
	mender::libarchive::wrapper::Handle archive_handle_;
#endif

	ExpectedSize Read(vector<uint8_t>::iterator start, vector<uint8_t>::iterator end) override;

public:
	Reader(io::Reader &reader);

	ExpectedEntry Next();
};

} // namespace tar
} // namespace mender

#endif // MENDER_COMMON_TAR_HPP
