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

#ifndef MENDER_ARTIFACT_V3_HEADER_PARSER_HPP
#define MENDER_ARTIFACT_V3_HEADER_PARSER_HPP

#include <string>
#include <utility>
#include <vector>
#include <unordered_map>

#include <common/optional.hpp>
#include <common/expected.hpp>
#include <common/error.hpp>
#include <common/io.hpp>

#include <artifact/config.hpp>

namespace mender {
namespace artifact {
namespace v3 {
namespace header {

using namespace std;


namespace optional = mender::common::optional;
namespace expected = mender::common::expected;
namespace io = mender::common::io;
namespace error = mender::common::error;

//
// +---header-info
//

enum class Payload {
	RootfsImage,
	ModuleImage,
	EmptyPayload,
};

struct PayloadType {
	Payload type;
	string name;
};

struct Provides {
	string artifact_name;
	optional::optional<string> artifact_group;
};

struct Depends {
	vector<string> device_type;
	optional::optional<vector<string>> artifact_name;
	optional::optional<vector<string>> artifact_group;
};

struct Info {
	vector<PayloadType> payloads;
	Provides provides;
	Depends depends;
};

using ExpectedHeaderInfo = expected::expected<header::Info, error::Error>;

namespace info {
ExpectedHeaderInfo Parse(io::Reader &reader);
}

//
// |    +---scripts
// |    |    |
// |    |    +---State_Enter
// |    |    +---State_Leave
// |    |    +---State_Error
// |    |    `---<more scripts>
//

using ArtifactScript = string;

//
// |    `---headers
// |         |
// |         +---0000
// |         |    |
// |         |    +---type-info
// |         |    |
// |         |    +---meta-data
// |         |
// |         +---0001
// |         |    |
// |         |    `---<more headers>
// |         |
// |         `---000n ...
//

struct TypeInfo {
	string type;
	optional::optional<unordered_map<string, string>> artifact_provides;
	optional::optional<unordered_map<string, string>> artifact_depends;
	optional::optional<vector<string>> clears_artifact_provides;
};

struct MetaData {};

struct SubHeader {
	TypeInfo type_info {};
	optional::optional<MetaData> metadata {};
};

namespace type_info {
using ExpectedTypeInfo = expected::expected<TypeInfo, error::Error>;

ExpectedTypeInfo Parse(io::Reader &reader);
} // namespace type_info

namespace meta_data {
using ExpectedMetaData = expected::expected<MetaData, error::Error>;

ExpectedMetaData Parse(io::Reader &reader);
} // namespace meta_data


// +---header.tar[.gz|.xz|.zst] (Optionally compressed)
// |    |
// |    +---header-info
// |    |
// |    +---scripts
// |    |    |
// |    |    +---State_Enter
// |    |    +---State_Leave
// |    |    +---State_Error
// |    |    `---<more scripts>
// |    |
// |    `---headers
// |         |
// |         +---0000
// |         |    |
// |         |    +---type-info
// |         |    |
// |         |    +---meta-data
// |         |
// |         +---0001
// |         |    |
// |         |    `---<more headers>
// |         |
// |         `---000n ...

struct Header {
	Info info;
	optional::optional<vector<ArtifactScript>> artifactScripts;
	vector<SubHeader> subHeaders {};
};

using ExpectedHeader = expected::expected<Header, error::Error>;

using ParserConfig = artifact::parser::config::ParserConfig;

ExpectedHeader Parse(io::Reader &reader, ParserConfig conf = {"./"});

} // namespace header
} // namespace v3
} // namespace artifact
} // namespace mender
#endif // MENDER_ARTIFACT_V3_HEADER_PARSER_HPP
