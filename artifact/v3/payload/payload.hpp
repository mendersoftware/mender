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

#ifndef MENDER_ARTIFACT_PAYLOAD_PARSER_HPP
#define MENDER_ARTIFACT_PAYLOAD_PARSER_HPP

#include <vector>
#include <string>
#include <memory>

#include <common/io.hpp>
#include <common/expected.hpp>

#include <artifact/sha/sha.hpp>
#include <artifact/tar/tar.hpp>

namespace mender {
namespace artifact {
namespace v3 {
namespace payload {

using namespace std;

namespace io = mender::common::io;
namespace error = mender::common::error;
namespace sha = mender::sha;
namespace expected = mender::common::expected;


using mender::common::expected::ExpectedSize;

class Reader : virtual public io::Reader {
public:
	Reader(io::Reader &reader, string name, size_t size) :
		reader_ {sha::Reader {reader, "TODO - shasum"}},
		name_ {name},
		size_ {size} {};
	ExpectedSize Read(vector<uint8_t>::iterator start, vector<uint8_t>::iterator end) override;

	Reader(Reader &&) = default;
	Reader &operator=(Reader &&reader) = default;

	string Name() {
		return this->name_;
	}
	size_t Size() {
		return this->size_;
	}

private:
	sha::Reader reader_;
	string name_;
	size_t size_;
};

using ExpectedPayloadReader = expected::expected<Reader, error::Error>;

class Payload {
public:
	Payload(io::Reader &reader) :
		tar_reader_ {make_shared<tar::Reader>(reader)} {};

	// ExpectedSize Read(vector<uint8_t>::iterator start, vector<uint8_t>::iterator end) override;

	ExpectedPayloadReader Next();

private:
	shared_ptr<tar::Reader> tar_reader_;
};

} // namespace payload
} // namespace v3
} // namespace artifact
} // namespace mender

#endif // MENDER_ARTIFACT_PAYLOAD_PARSER_HPP
