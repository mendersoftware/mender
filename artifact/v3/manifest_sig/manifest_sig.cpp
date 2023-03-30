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

#include <artifact/v3/manifest_sig/manifest_sig.hpp>
#include <sstream>
#include <common/error.hpp>
#include <artifact/error.hpp>

namespace mender {
namespace artifact {
namespace v3 {
namespace manifest_sig {

namespace io = mender::common::io;
namespace error = mender::common::error;

ExpectedManifestSignature Parse(io::Reader &reader) {
	stringstream ss;
	io::StreamWriter sw {ss};

	auto err = io::Copy(sw, reader);
	if (error::NoError != err) {
		return expected::unexpected(err);
	}

	ManifestSignature mw(ss.str());
	return mw;
}

} // namespace manifest_sig
} // namespace v3
} // namespace artifact
} // namespace mender
