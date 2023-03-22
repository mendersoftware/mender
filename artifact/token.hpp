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

#ifndef MENDER_ARTIFACT_TOKEN_HPP
#define MENDER_ARTIFACT_TOKEN_HPP

#include <unordered_map>
#include <string>

#include <common/log.hpp>
#include <common/expected.hpp>

namespace mender {
namespace artifact {
namespace token {

using namespace std;

namespace tar = mender::tar;

namespace log = mender::common::log;
namespace error = mender::common::error;
namespace expected = mender::common::expected;

enum class Type {
	Uninitialized = 0,
	Unrecognized,
	Version,
	Manifest,
	ManifestSignature,
	ManifestAugment,
	Header,
	HeaderAugment,
	Payload,
};

const unordered_map<const Type, const string> type_map {
	{Type::Uninitialized, "Uninitialized"},
	{Type::Unrecognized, "Unrecognized"},
	{Type::Version, "version"},
	{Type::Manifest, "manifest"},
	{Type::ManifestAugment, "manifest-augment"},
	{Type::ManifestSignature, "manifest.sig"},
	{Type::Header, "header"},
	{Type::HeaderAugment, "header-augment"},
	{Type::Payload, "data"},
};

class Token {
public:
	Type type;
	// Poor mans optional
	shared_ptr<tar::Entry> value;

public:
	Token() :
		type {Type::Uninitialized} {
	}
	Token(Type t) :
		type {t} {
	}
	Token(const string &type_name, tar::Entry &entry) :
		type {StringToType(type_name)},
		value {make_shared<tar::Entry>(entry)} {
	}

	const string TypeToString() const {
		return type_map.at(type);
	}

private:
	Type StringToType(const string &type_name) {
		if (type_name == "version") {
			return Type::Version;
		}
		if (type_name == "manifest") {
			return Type::Manifest;
		}
		if (type_name.find("header.tar") == 0) {
			return Type::Header;
		}
		if (type_name.find("data/0000.tar") == 0) {
			return Type::Payload;
		}
		log::Error("Unrecognized token: " + type_name);
		return Type::Unrecognized;
	}
};
} // namespace token
} // namespace artifact
} // namespace mender

#endif // MENDER_ARTIFACT_TOKEN_HPP
