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

#ifndef MENDER_COMMON_HTTP_TEST_HELPERS_HPP
#define MENDER_COMMON_HTTP_TEST_HELPERS_HPP

#include <vector>

#include <common/expected.hpp>
#include <common/io.hpp>

using namespace std;

namespace expected = mender::common::expected;
namespace io = mender::common::io;

class BodyOfXes : virtual public io::Reader {
public:
	expected::ExpectedSize Read(
		vector<uint8_t>::iterator start, vector<uint8_t>::iterator end) override {
		auto iter_count = end - start;
		size_t read;
		if (iter_count + count_ > TARGET_BODY_SIZE) {
			read = TARGET_BODY_SIZE - count_;
		} else {
			read = iter_count;
		}

		for (size_t i = 0; i < read; i++) {
			start[i] = TransferFunction(count_ + i);
		}
		count_ += read;
		return read;
	}

	static uint8_t TransferFunction(size_t index) {
		// Fill in a specific pattern to try to catch offset/block errors: Raise the input
		// number to the power of 1.1 and round to the nearest integer. If it's odd, return
		// 'X', if it's even, return 'x'. Due to the exponent, this pattern will change
		// slightly throughout the sequence, giving us a chance to catch offset errors.
		// (Note: Try printing it, the pattern is mesmerizing to watch!)
		auto num = size_t(round(pow(index, 1.1)));
		if (num & 1) {
			return 'X';
		} else {
			return 'x';
		}
	}

	// Just some random size, but preferably big, and not falling on a block boundary.
	static const size_t TARGET_BODY_SIZE {1234567};

private:
	size_t count_;
};
const size_t BodyOfXes::TARGET_BODY_SIZE;

#endif // MENDER_COMMON_HTTP_TEST_HELPERS_HPP
