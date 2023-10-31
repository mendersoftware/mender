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

#include <mender-update/progress_reader/progress_reader.hpp>

#include <vector>
#include <cmath>
#include <cstdio>

namespace mender {
namespace update {
namespace progress {

expected::ExpectedSize Reader::Read(
	vector<uint8_t>::iterator start, vector<uint8_t>::iterator end) {
	expected::ExpectedSize exp_read = reader_->Read(start, end);
	if (exp_read) {
		bytes_read_ += exp_read.value();
		int percentage = (bytes_read_ / static_cast<float>(tot_size_)) * 100;
		if (percentage > last_percentage) {
			cerr << "\r" << percentage << "%";
		}
	}
	return exp_read;
}

} // namespace progress
} // namespace update
} // namespace mender
