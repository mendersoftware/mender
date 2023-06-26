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
#include <artifact/sha/sha.hpp>

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
namespace manifest_sig = mender::artifact::v3::manifest_sig;
namespace payload = mender::artifact::v3::payload;

ExpectedArtifact VerifyEmptyPayloadArtifact(
	Artifact &artifact, lexer::Lexer<token::Token, token::Type> &lexer) {
	// No meta-data allowed
	if (artifact.header.subHeaders.at(0).metadata) {
		return expected::unexpected(parser_error::MakeError(
			parser_error::Code::ParseError,
			"Empty payload Artifacts cannot contain a meta-data section"));
	}
	// TODO - When augmented sections are added - check for these also
	log::Trace("Empty payload Artifact: Verifying empty payload");
	auto tok = lexer.Next();
	if (tok.type == token::Type::Payload) {
		// If a payload is present, verify that it is empty
		auto expected_payload = artifact.Next();
		if (!expected_payload) {
			auto err = expected_payload.error();
			if (err.code.value() != parser_error::Code::NoMorePayloadFilesError) {
				return expected::unexpected(parser_error::MakeError(
					parser_error::Code::ParseError,
					"This should never happen, we have a payload token / programmer error"));
			}
			return artifact;
		}
		auto payload = expected_payload.value();
		auto expected_payload_file = payload.Next();
		if (expected_payload_file) {
			return expected::unexpected(parser_error::MakeError(
				parser_error::Code::ParseError, "Empty Payload Artifacts cannot have a payload"));
		}
	}
	return artifact;
}

ExpectedArtifact Parse(io::Reader &reader, config::ParserConfig config) {
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
	optional::optional<ManifestSignature> signature;
	if (tok.type == token::Type::ManifestSignature) {
		auto expected_signature = manifest_sig::Parse(*tok.value);
		if (!expected_signature) {
			return expected::unexpected(parser_error::MakeError(
				parser_error::Code::ParseError,
				"Failed to parse the manifest signature: " + expected_signature.error().message));
		}
		signature = expected_signature.value();
		tok = lexer.Next();

		// Verify the signature
		if (config.artifact_verify_keys.size() > 0) {
			auto expected_verified = manifest_sig::VerifySignature(
				*signature, manifest.GetShaSum(), config.artifact_verify_keys);
			if (!expected_verified) {
				return expected::unexpected(parser_error::MakeError(
					parser_error::Code::SignatureVerificationError,
					"Failed to verify the manifest signature: "
						+ expected_verified.error().message));
			}
			if (!expected_verified.value()) {
				return expected::unexpected(parser_error::MakeError(
					parser_error::Code::SignatureVerificationError,
					"Wrong manifest signature or wrong key"));
			}
		}
	}

	log::Trace("Parsing the Header");
	if (tok.type != token::Type::Header) {
		return expected::unexpected(parser_error::MakeError(
			parser_error::Code::ParseError,
			"Got unexpected token " + tok.TypeToString() + " expected 'Header'"));
	}
	sha::Reader shasum_reader {*tok.value, manifest.Get("header.tar")};
	auto expected_header = v3::header::Parse(shasum_reader, config);
	if (!expected_header) {
		return expected::unexpected(parser_error::MakeError(
			parser_error::Code::ParseError,
			"Failed to parse the header: " + expected_header.error().message));
	}
	auto header = expected_header.value();

	// Create the object
	auto artifact = Artifact {version, manifest, header, lexer};
	if (signature) {
		artifact.manifest_signature = signature;
	}

	// Check the empty payload structure
	if (header.info.payloads.at(0).type == v3::header::Payload::EmptyPayload) {
		auto expected_empty_payload_artifact = VerifyEmptyPayloadArtifact(artifact, lexer);
		if (!expected_empty_payload_artifact) {
			return expected_empty_payload_artifact;
		}
		return artifact;
	}

	log::Trace("Parsing the payload");
	tok = lexer.Next();
	if (tok.type != token::Type::Payload) {
		return expected::unexpected(parser_error::MakeError(
			parser_error::Code::ParseError,
			"Got unexpected token " + tok.TypeToString() + " expected 'data/0000.tar"));
	}

	return artifact;
};


ExpectedPayload Artifact::Next() {
	// Currently only one payload supported
	if (payload_index_ != 0) {
		return expected::unexpected(parser_error::MakeError(
			parser_error::Code::EOFError, "Reached the end of the Artifact"));
	}
	payload_index_++;
	return payload::Payload(*(this->lexer_.current.value), manifest);
}

} // namespace parser
} // namespace artifact
} // namespace mender
