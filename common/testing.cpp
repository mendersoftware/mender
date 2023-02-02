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

#include <common/testing.hpp>

#include <filesystem>
#include <random>

namespace mender {
namespace common {
namespace testing {

namespace fs = std::filesystem;

TemporaryDirectory::TemporaryDirectory() {
	fs::path path = fs::temp_directory_path();
	path.append("mender-test-" + std::to_string(std::random_device()()));
	fs::create_directories(path);
	path_ = path;
}

TemporaryDirectory::~TemporaryDirectory() {
	fs::remove_all(path_);
}

std::string TemporaryDirectory::Path() {
	return path_;
}

} // namespace testing
} // namespace common
} // namespace mender
