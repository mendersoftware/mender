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

ExpectedSize ByteWriter::Write(
	vector<uint8_t>::const_iterator start, vector<uint8_t>::const_iterator end) {
	assert(end > start);
	Vsize max_write {receiver_.size() - bytes_written_};
	if (max_write == 0) {
		return Error(make_error_condition(errc::no_space_on_device), "");
	}
	Vsize iterator_size {static_cast<Vsize>(end - start)};
	Vsize bytes_to_write {min(iterator_size, max_write)};
	auto it = next(receiver_.begin(), bytes_written_);
	std::copy_n(start, bytes_to_write, it);
	bytes_written_ += bytes_to_write;
	return bytes_to_write;
}

} // namespace io
} // namespace common
} // namespace mender
