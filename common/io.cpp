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

namespace mender {
namespace common {
namespace io {

Error Copy(Writer &dst, Reader &src) {
	vector<uint8_t> buffer(4096);
	return Copy(dst, src, buffer);
}

Error Copy(Writer &dst, Reader &src, vector<uint8_t> &buffer) {
	size_t orig_size = buffer.size();

	while (true) {
		if (buffer.size() != orig_size) {
			buffer.resize(orig_size);
		}

		auto result = src.Read(buffer);
		if (!result) {
			return result.error();
		} else if (result.value() == 0) {
			return NoError;
		} else if (result.value() > buffer.size()) {
			return error::MakeError(
				error::ProgrammingError,
				"Read returned more bytes than requested. This is a bug in the Read function.");
		}

		if (result.value() != buffer.size()) {
			// Because we only ever resize down, this should be very cheap. Resizing
			// back up to capacity below is then also cheap.
			buffer.resize(result.value());
		}

		result = dst.Write(buffer);
		if (!result) {
			return result.error();
		} else if (result.value() == 0) {
			// Should this even happen?
			return Error(std::error_condition(std::errc::io_error), "Zero write when copying data");
		} else if (result.value() != buffer.size()) {
			return Error(
				std::error_condition(std::errc::io_error), "Short write when copying data");
		}
	}
}

} // namespace io
} // namespace common
} // namespace mender
