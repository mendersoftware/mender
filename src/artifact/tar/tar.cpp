// Copyright 2024 Northern.tech AS
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

#include <artifact/tar/tar.hpp>

#include <common/io.hpp>

namespace mender {
namespace tar {

using namespace std;

namespace expected = mender::common::expected;

using ExpectedSize = expected::ExpectedSize;

ExpectedSize Entry::Read(vector<uint8_t>::iterator start, vector<uint8_t>::iterator end) {
	ExpectedSize read_bytes = reader_.Read(start, end);

	if (!read_bytes) {
		return read_bytes;
	}

	nr_bytes_read_ += read_bytes.value();

	return read_bytes;
}

} // namespace tar
} // namespace mender
