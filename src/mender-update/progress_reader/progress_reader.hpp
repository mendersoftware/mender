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

#include <memory>
#include <vector>

#include <common/expected.hpp>
#include <common/io.hpp>

namespace mender {
namespace update {
namespace progress {

using namespace std;

namespace io = mender::common::io;
namespace expected = mender::common::expected;

class Reader : virtual public io::Reader {
public:
	Reader(const shared_ptr<io::Reader> &reader, size_t size) :
		reader_ {reader},
		tot_size_ {size} {};

	expected::ExpectedSize Read(
		vector<uint8_t>::iterator start, vector<uint8_t>::iterator end) override;

private:
	shared_ptr<io::Reader> reader_;
	size_t tot_size_;
	size_t bytes_read_ {0};
	int last_percentage {-1};
};

} // namespace progress
} // namespace update
} // namespace mender
