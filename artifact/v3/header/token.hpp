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

#ifndef MENDER_ARTIFACT_V3_HEADER_TOKEN_HPP
#define MENDER_ARTIFACT_V3_HEADER_TOKEN_HPP


#include <unordered_map>
#include <string>
#include <regex>
#include <memory>

#include <common/log.hpp>
#include <common/expected.hpp>

const std::regex artifact_script_regexp {
	"scripts/Artifact(Install|Reboot|Rollback|RollbackReboot|Commit|Failure)_(Enter|Leave|Error)_[0-9][0-9](_\\S+)?",
	std::regex_constants::ECMAScript};

const int artifact_headers_index_position {8};
const int artifact_headers_index_length {4};

namespace mender {
namespace artifact {
namespace v3 {
namespace header {
namespace token {

using namespace std;

namespace tar = mender::tar;

namespace log = mender::common::log;
namespace error = mender::common::error;
namespace expected = mender::common::expected;

enum class Type {
	Uninitialized = 0,
	EOFToken,
	Unrecognized,
	HeaderInfo,
	ArtifactScripts,
	ArtifactHeaderTypeInfo,
	ArtifactHeaderMetaData,
};

unordered_map<const Type, string> type_map {
	{Type::Uninitialized, "Uninitialized"},
	{Type::EOFToken, "EOF"},
	{Type::Unrecognized, "Unrecognized"},
	{Type::HeaderInfo, "header-info"},
	{Type::ArtifactScripts, "artifact-scripts"},
	{Type::ArtifactHeaderTypeInfo, "type-info"},
	{Type::ArtifactHeaderMetaData, "header-meta-data"},
};

class Token {
public:
	Type type;
	string name {};
	shared_ptr<tar::Entry> value;

	Token() :
		type {Type::Uninitialized} {
	}
	explicit Token(Type t) :
		type {t} {
	}
	Token(const string &type_name, tar::Entry &entry) :
		type {StringToType(type_name)},
		name {StringToName(type_name)},
		value {make_shared<tar::Entry>(entry)} {
	}

	const string TypeToString() const {
		return type_map.at(type);
	}

	const int Index() const {
		switch (type) {
		case Type::ArtifactHeaderTypeInfo:
			// fallthrough
		case Type::ArtifactHeaderMetaData:
			return stoi(
				this->name.substr(artifact_headers_index_position, artifact_headers_index_length));
			break;
		default:
			return -1;
		}
	}

private:
	Type StringToType(const string &type_name) {
		log::Trace("StringToType: " + type_name);
		if (type_name == "header-info") {
			return Type::HeaderInfo;
		}
		if (std::regex_match(type_name, artifact_script_regexp)) {
			return Type::ArtifactScripts;
		}
		if (std::regex_match(type_name, regex("headers/[0-9]{4}/type-info"))) {
			return Type::ArtifactHeaderTypeInfo;
		}
		if (regex_match(type_name, regex("headers/[0-9]{4}/meta-data"))) {
			return Type::ArtifactHeaderMetaData;
		}
		log::Error("Unrecognized token: " + type_name);
		return Type::Unrecognized;
	}

	string StringToName(const string &type_name) {
		if (std::regex_match(type_name, artifact_script_regexp)) {
			return type_name.substr(7 + 1); // Strip the scripts + / prefix
		}
		return type_name;
	}
};

} // namespace token
} // namespace header
} // namespace v3
} // namespace artifact
} // namespace mender

#endif // MENDER_ARTIFACT_V3_HEADER_TOKEN_HPP
