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

#include <artifact/v3/header/header.hpp>

#include <iomanip>
#include <memory>
#include <string>
#include <system_error>
#include <vector>
#include <iostream>
#include <fstream>

#include <common/expected.hpp>
#include <common/error.hpp>
#include <common/io.hpp>
#include <common/log.hpp>
#include <common/json.hpp>
#include <common/common.hpp>
#include <common/path.hpp>

#include <artifact/error.hpp>
#include <artifact/lexer.hpp>
#include <artifact/tar/tar.hpp>

#include <artifact/v3/header/token.hpp>

namespace mender {
namespace artifact {
namespace v3 {
namespace header {

using namespace std;

namespace expected = mender::common::expected;
namespace io = mender::common::io;
namespace error = mender::common::error;
namespace log = mender::common::log;
namespace json = mender::common::json;
namespace path = mender::common::path;


namespace {
string IndexString(int index) {
	stringstream index_string {};
	index_string << setw(4) << setfill('0') << index;
	return index_string.str();
}
} // namespace

ExpectedHeader Parse(io::Reader &reader, ParserConfig conf) {
	Header header {};

	shared_ptr<tar::Reader> tar_reader {make_shared<tar::Reader>(reader)};

	auto lexer = lexer::Lexer<header::token::Token, header::token::Type> {tar_reader};

	token::Token tok = lexer.Next();

	if (tok.type != token::Type::HeaderInfo) {
		return expected::unexpected(parser_error::MakeError(
			parser_error::Code::ParseError,
			"Got unexpected token: '" + tok.TypeToString() + "' expected 'header-info'"));
	}

	auto expected_info = header::info::Parse(*tok.value);

	if (!expected_info) {
		return expected::unexpected(parser_error::MakeError(
			parser_error::Code::ParseError,
			"Failed to parse the header-info: " + expected_info.error().message));
	}
	header.info = expected_info.value();

	tok = lexer.Next();
	vector<ArtifactScript> state_scripts {};
	if (tok.type == token::Type::ArtifactScripts) {
		if (not path::FileExists(conf.artifact_scripts_filesystem_path)) {
			log::Trace(
				"Creating the Artifact script directory: " + conf.artifact_scripts_filesystem_path);
			error::Error err = path::CreateDirectories(conf.artifact_scripts_filesystem_path);
			if (err != error::NoError) {
				return expected::unexpected(err.WithContext(
					"Failed to create the scripts directory for installing Artifact scripts"));
			}
		}
	}
	while (tok.type == token::Type::ArtifactScripts) {
		log::Trace("Parsing state script...");
		const string artifact_script_path =
			path::Join(conf.artifact_scripts_filesystem_path, tok.name);
		errno = 0;
		ofstream myfile(artifact_script_path);
		log::Trace("state script name: " + tok.name);
		if (!myfile.good()) {
			auto io_errno = errno;
			return expected::unexpected(error::Error(
				std::generic_category().default_error_condition(io_errno),
				"Failed to create a file for writing the Artifact script: " + artifact_script_path
					+ " to the filesystem"));
		}
		io::StreamWriter sw {myfile};

		auto err = io::Copy(sw, *tok.value);
		if (err != error::NoError) {
			return expected::unexpected(err);
		}

		state_scripts.push_back(artifact_script_path);

		// Set the permissions on the installed Artifact scripts
		err = path::Permissions(
			artifact_script_path,
			{path::Perms::Owner_read, path::Perms::Owner_write, path::Perms::Owner_exec});
		if (err != error::NoError) {
			return expected::unexpected(err);
		}

		tok = lexer.Next();
	}

	if (state_scripts.size() > 0) {
		// Write the Artifact script version file
		const string artifact_script_version_file =
			path::Join(conf.artifact_scripts_filesystem_path, "version");
		errno = 0;
		ofstream myfile(artifact_script_version_file);
		log::Trace("Creating the Artifact script version file: " + artifact_script_version_file);
		if (!myfile.good()) {
			auto io_errno = errno;
			return expected::unexpected(error::Error(
				std::generic_category().default_error_condition(io_errno),
				"Failed to create the Artifact script version file: "
					+ artifact_script_version_file));
		}
		myfile << to_string(conf.artifact_scripts_version);
		if (!myfile.good()) {
			auto io_errno = errno;
			return expected::unexpected(error::Error(
				std::generic_category().default_error_condition(io_errno),
				"I/O error writing the Artifact scripts version file"));
		}

		// Sync the directory so we know it is permanent.
		auto err = path::DataSyncRecursively(conf.artifact_scripts_filesystem_path);
		if (err != error::NoError) {
			return expected::unexpected(err.WithContext("While syncing artifact script directory"));
		}
	}


	header.artifactScripts = std::move(state_scripts);

	vector<SubHeader> subheaders {};

	int current_index {0};
	while (tok.type != token::Type::EOFToken) {
		log::Trace("Parsing the sub-header ...");

		// NOTE: We currently do not support multiple payloads
		if (current_index != 0) {
			return expected::unexpected(parser_error::MakeError(
				parser_error::Code::ParseError,
				"Multiple header entries found. Currently only one is supported"));
		}

		SubHeader sub_header {};
		if (tok.type != token::Type::ArtifactHeaderTypeInfo) {
			return expected::unexpected(parser_error::MakeError(
				parser_error::Code::ParseError,
				"Unexpected entry: " + tok.TypeToString() + " expected: type-info"));
		}

		if (current_index != tok.Index()) {
			return expected::unexpected(parser_error::MakeError(
				parser_error::Code::ParseError,
				"Unexpected index order for the type-info: " + tok.name + " expected: headers/"
					+ IndexString(current_index) + "/type-info"));
		}
		auto expected_type_info = type_info::Parse(*tok.value);
		if (!expected_type_info) {
			return expected::unexpected(expected_type_info.error());
		}
		sub_header.type_info = expected_type_info.value();

		// NOTE (workaround): Bug in the Artifact format writer:
		// If the type is a RootfsImage, then the payload-type will be empty. This
		// is a bug in the mender-artifact tool, which writes the payload. For now,
		// just work around it.
		if (header.info.payloads[current_index].type == Payload::RootfsImage) {
			log::Debug(
				"Setting the type-info in payload nr " + to_string(current_index)
				+ " to rootfs-image");
			sub_header.type_info.type = "rootfs-image";
		}

		tok = lexer.Next();

		log::Trace("sub-header: looking for meta-data");

		// meta-data (optional)
		if (tok.type == token::Type::ArtifactHeaderMetaData) {
			if (current_index != tok.Index()) {
				return expected::unexpected(parser_error::MakeError(
					parser_error::Code::ParseError,
					"Unexpected index order for the meta-data: " + tok.name + " expected: headers/"
						+ IndexString(current_index) + "/meta-data"));
			}
			auto expected_meta_data = meta_data::Parse(*tok.value);
			if (!expected_meta_data) {
				return expected::unexpected(expected_meta_data.error());
			}
			sub_header.metadata = expected_meta_data.value();
			tok = lexer.Next();
		}
		log::Trace("sub-header: parsed the meta-data");

		header.subHeaders.push_back(sub_header);

		current_index++;
	}

	return header;
}

} // namespace header
} // namespace v3
} // namespace artifact
} // namespace mender
