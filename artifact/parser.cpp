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

#include <artifact/parser.hpp>

#include <cstdint>
#include <memory>
#include <system_error>
#include <unordered_map>

#include <common/json.hpp>
#include <common/log.hpp>
#include <artifact/tar/tar.hpp>
#include <common/common.hpp>

#include <artifact/lexer.hpp>
#include <artifact/tar/tar.hpp>

namespace mender {
namespace artifact {
namespace v3 {
namespace header {
Header Parse() {
	return Header {};
}
} // namespace header
} // namespace v3
} // namespace artifact
} // namespace mender


namespace mender {
namespace artifact {
namespace parser {

using namespace std;

namespace lexer = artifact::lexer;
namespace log = mender::common::log;
namespace io = mender::common::io;
namespace tar = mender::tar;
namespace expected = mender::common::expected;
namespace error = mender::common::error;

namespace version = mender::artifact::v3::version;
namespace manifest = mender::artifact::v3::manifest;
namespace payload = mender::artifact::v3::payload;

ExpectedArtifact Parse(io::Reader &reader) {
	std::shared_ptr<tar::Reader> tar_reader {make_shared<tar::Reader>(reader)};

	auto lexer = lexer::Lexer<token::Token, token::Type> {tar_reader};

	token::Token tok = lexer.Next();

	log::Trace("Parsing Version");
	if (tok.type != token::Type::Version) {
		return expected::unexpected(parser_error::MakeError(
			parser_error::Code::ParseError,
			"Got unexpected token : '" + tok.TypeToString() + "' expected 'version'"));
	}

	auto expected_version = version::Parse(*tok.value);

	if (!expected_version) {
		return expected::unexpected(parser_error::MakeError(
			parser_error::Code::ParseError,
			"Failed to parse the version: " + expected_version.error().message));
	}

	auto version = expected_version.value();

	log::Trace("Parsing the Manifest");
	tok = lexer.Next();
	if (tok.type != token::Type::Manifest) {
		return expected::unexpected(parser_error::MakeError(
			parser_error::Code::ParseError,
			"Got unexpected token " + tok.TypeToString() + " expected 'manifest'"));
	}
	auto expected_manifest = manifest::Parse(*tok.value);
	if (!expected_manifest) {
		return expected::unexpected(parser_error::MakeError(
			parser_error::Code::ParseError,
			"Failed to parse the manifest: " + expected_manifest.error().message));
	}
	auto manifest = expected_manifest.value();

	tok = lexer.Next();
	if (tok.type == token::Type::ManifestSignature) {
		return expected::unexpected(parser_error::MakeError(
			parser_error::Code::ParseError, "Signed Artifacts are unsupported"));
	}

	log::Trace("Parsing the Header");
	if (tok.type != token::Type::Header) {
		return expected::unexpected(parser_error::MakeError(
			parser_error::Code::ParseError,
			"Got unexpected token " + tok.TypeToString() + " expected 'Header'"));
	}
	// auto header = v3::header::Parse(); // TODO (MEN-6178): Enable

	log::Trace("Parsing the payload");
	tok = lexer.Next();
	if (tok.type != token::Type::Payload) {
		return expected::unexpected(parser_error::MakeError(
			parser_error::Code::ParseError,
			"Got unexpected token " + tok.TypeToString() + "expected 'data/0000.tar"));
	}

	return Artifact {version, manifest, lexer}; // TODO (MEN-6178): Add the header
};


ExpectedPayloadReader Artifact::Next() {
	// Currently only one payload supported
	if (payload_index_ != 0) {
		return expected::unexpected(parser_error::MakeError(
			parser_error::Code::EOFError, "Reached the end of the Artifact"));
	}
	payload_index_++;
	return payload::Verify(*(this->lexer_.current.value), this->manifest.Get("data/0000.tar"));
}

} // namespace parser
} // namespace artifact
} // namespace mender
