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

#include <artifact/v3/payload/payload.hpp>

#include <vector>

#include <common/io.hpp>

namespace mender {
namespace artifact {
namespace v3 {
namespace payload {

using namespace std;


ExpectedSize Reader::Read(vector<uint8_t>::iterator start, vector<uint8_t>::iterator end) {
	return reader_.Read(start, end);
}

ExpectedPayloadReader Payload::Next() {
	auto ep {tar_reader_->Next()};
	if (!ep) {
		// TODO
		return expected::unexpected(error::MakeError(error::GenericError, ep.error().message));
	}
	auto p {ep.value()};
	return Reader {p, p.Name(), p.Size()};
}

} // namespace payload
} // namespace v3
} // namespace artifact
} // namespace mender
