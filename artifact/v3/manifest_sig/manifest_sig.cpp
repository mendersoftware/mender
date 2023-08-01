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
#include <common/crypto.hpp>

namespace mender {
namespace artifact {
namespace v3 {
namespace manifest_sig {

namespace io = mender::common::io;
namespace error = mender::common::error;
namespace crypto = mender::common::crypto;

ExpectedManifestSignature Parse(io::Reader &reader) {
	stringstream ss;
	io::StreamWriter sw {ss};

	auto err = io::Copy(sw, reader);
	if (error::NoError != err) {
		return expected::unexpected(err);
	}

	return ss.str();
}

expected::ExpectedBool VerifySignature(
	const ManifestSignature &signature,
	const mender::sha::SHA &shasum,
	const vector<string> &artifact_verify_keys) {
	error::Error err;
	for (const auto &key : artifact_verify_keys) {
		auto e_verify_sign = crypto::VerifySign(key, shasum, signature);
		if (e_verify_sign && e_verify_sign.value()) {
			return true;
		}
		if (!e_verify_sign) {
			err = err.FollowedBy(e_verify_sign.error());
		}
	}
	if (err == error::NoError) {
		return false;
	}
	return expected::unexpected(err);
}

} // namespace manifest_sig
} // namespace v3
} // namespace artifact
} // namespace mender
