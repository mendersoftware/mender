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

#include <common/io.hpp>
#include <common/path.hpp>

#include <functional>
#include <iterator>
#include <string>

namespace mender {
namespace common {
namespace path {

using namespace std;
namespace io = mender::common::io;

expected::ExpectedBool AreFilesIdentical(const string &file_one, const string &file_two) {
	auto file_one_expected_stream = io::OpenIfstream(file_one);
	if (!file_one_expected_stream) {
		return expected::unexpected(file_one_expected_stream.error());
	}
	auto file_two_expected_stream = io::OpenIfstream(file_two);
	if (!file_two_expected_stream) {
		return expected::unexpected(file_two_expected_stream.error());
	}

	auto &file_one_stream = file_one_expected_stream.value();
	auto &file_two_stream = file_two_expected_stream.value();

	// Compare file sizes
	file_one_stream.seekg(0, ios::end);
	file_two_stream.seekg(0, ios::end);
	if (file_one_stream.tellg() != file_two_stream.tellg()) {
		return false;
	}
	file_one_stream.seekg(0, ios::beg);
	file_two_stream.seekg(0, ios::beg);

	string file_one_contents {
		istreambuf_iterator<char>(file_one_stream), istreambuf_iterator<char>()};
	string file_two_contents {
		istreambuf_iterator<char>(file_two_stream), istreambuf_iterator<char>()};

	size_t file_one_hash = std::hash<string> {}(file_one_contents);
	size_t file_two_hash = std::hash<string> {}(file_two_contents);


	return file_one_hash == file_two_hash;
}

} // namespace path
} // namespace common
} // namespace mender
