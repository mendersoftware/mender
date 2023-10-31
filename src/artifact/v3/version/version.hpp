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

#ifndef MENDER_ARTIFACT_VERSION_PARSER_HPP
#define MENDER_ARTIFACT_VERSION_PARSER_HPP

#include <string>

#include <common/expected.hpp>
#include <common/error.hpp>
#include <common/io.hpp>

namespace mender {
namespace artifact {
namespace v3 {
namespace version {

using namespace std;

namespace io = mender::common::io;
namespace expected = mender::common::expected;
namespace error = mender::common::error;


enum ErrorCode {
	NoError = 0,
	ParseError,
	VersionError,
	FormatError,
};

class ErrorCategoryClass : public std::error_category {
public:
	const char *name() const noexcept override;
	string message(int code) const override;
};

extern const ErrorCategoryClass ErrorCategory;

error::Error MakeError(ErrorCode code, const string &msg);

struct Version {
	const int version;
	const string format;
};

using ExpectedVersion = expected::expected<Version, error::Error>;

ExpectedVersion Parse(io::Reader &reader);

} // namespace version
} // namespace v3
} // namespace artifact
} // namespace mender

#endif // MENDER_ARTIFACT_VERSION_PARSER_HPP
