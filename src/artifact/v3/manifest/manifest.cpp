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

#include <artifact/v3/manifest/manifest.hpp>

#include <cctype>
#include <iterator>
#include <string>
#include <sstream>
#include <regex>

#include <common/common.hpp>
#include <common/error.hpp>
#include <artifact/sha/sha.hpp>

#include <artifact/error.hpp>

namespace mender {
namespace artifact {
namespace v3 {
namespace manifest {

namespace io = mender::common::io;
namespace sha = mender::sha;
namespace error = mender::common::error;

struct ManifestLine {
	const string shasum;
	const string entry_name;
};

using ExpectedManifestLine = expected::expected<ManifestLine, error::Error>;

const size_t expected_shasum_length {64};
const size_t expected_whitespace {2};
const string manifest_line_regex_string {
	"^([0-9a-z]{" + to_string(expected_shasum_length) + "})[[:space:]]{"
	+ to_string(expected_whitespace) + "}([^[:blank:]]+)$"};

const vector<string> supported_compression_suffixes {".gz", ".xz", ".zst"};

namespace {

string MaybeStripSuffix(string s, vector<string> suffixes) {
	auto s_ {s};
	for (const auto &suffix : suffixes) {
		if (s.substr(s.size() - suffix.size()) == suffix) {
			s_.erase(s.size() - suffix.size());
			return s_;
		}
	}
	return s_;
}

ExpectedManifestLine Tokenize(const string &line) {
	const std::regex manifest_line_regex(
		manifest_line_regex_string, std::regex_constants::ECMAScript);
	std::smatch base_match;
	std::regex_match(line, base_match, manifest_line_regex);

	if (base_match.size() != 3) {
		return expected::unexpected(parser_error::MakeError(
			parser_error::ParseError,
			"Line (" + line
				+ ") is not in the expected manifest format: " + manifest_line_regex_string));
	}

	return ManifestLine {
		.shasum = base_match[1],
		.entry_name = MaybeStripSuffix(base_match[2], supported_compression_suffixes)};
}
} // namespace

ExpectedManifest Parse(mender::common::io::Reader &reader) {
	Manifest m {};

	sha::Reader sha_reader {reader};
	vector<uint8_t> data {};
	auto byte_writer = io::ByteWriter(data);
	byte_writer.SetUnlimited(true);

	auto err = io::Copy(byte_writer, sha_reader);
	if (error::NoError != err) {
		return expected::unexpected(err);
	}
	auto expected_sha = sha_reader.ShaSum();
	if (!expected_sha) {
		expected::unexpected(parser_error::MakeError(
			parser_error::ParseError, "Invalid ShaSum: " + expected_sha.error().message));
	}
	m.shasum_ = expected_sha.value();

	std::stringstream input {common::StringFromByteVector(data)};
	string line {};
	while (getline(input, line, '\n')) {
		auto manifest_line = Tokenize(line);
		if (!manifest_line) {
			return expected::unexpected(manifest_line.error());
		}

		m.map_[manifest_line->entry_name] = manifest_line->shasum;
	}

	return m;
}

string Manifest::Get(const string &key) {
	auto value = this->map_.find(key);
	if (value != this->map_.end()) {
		return value->second;
	}
	return "";
}

} // namespace manifest
} // namespace v3
} // namespace artifact
} // namespace mender
