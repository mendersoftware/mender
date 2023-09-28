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

#ifndef MENDER_ARTIFACT_PARSER_HPP
#define MENDER_ARTIFACT_PARSER_HPP

#include <memory>
#include <unordered_map>

#include <common/optional.hpp>
#include <common/expected.hpp>
#include <common/error.hpp>
#include <common/log.hpp>
#include <common/io.hpp>
#include <artifact/tar/tar.hpp>

#include <artifact/sha/sha.hpp>

#include <artifact/v3/version/version.hpp>
#include <artifact/v3/manifest/manifest.hpp>
#include <artifact/v3/header/header.hpp>
#include <artifact/v3/manifest_sig/manifest_sig.hpp>
#include <artifact/v3/payload/payload.hpp>

#include <artifact/lexer.hpp>
#include <artifact/token.hpp>
#include <artifact/config.hpp>

#include <artifact/error.hpp>

namespace mender {
namespace artifact {
namespace parser {

using namespace std;

namespace expected = mender::common::expected;
namespace error = mender::common::error;
namespace io = mender::common::io;

namespace payload = mender::artifact::v3::payload;

namespace tar = mender::tar;

using Version = mender::artifact::v3::version::Version;
using Manifest = mender::artifact::v3::manifest::Manifest;
using ManifestSignature = mender::artifact::v3::manifest_sig::ManifestSignature;
using Header = mender::artifact::v3::header::Header;

namespace payload = mender::artifact::v3::payload;

using ExpectedPayload = expected::expected<payload::Payload, error::Error>;

// Structure to hold the contents of a Mender artifact file.
class Artifact {
private:
	lexer::Lexer<token::Token, token::Type> lexer_;
	unsigned int payload_index_ {0};

public:
	Version version;
	Manifest manifest;
	optional<ManifestSignature> manifest_signature {};
	Header header {};

	ExpectedPayload Next();

	Artifact(
		Version &version,
		Manifest &manifest,
		Header &header,
		lexer::Lexer<token::Token, token::Type> lexer) :
		lexer_ {lexer},
		version {version},
		manifest {manifest},
		header {header} {
	}
};

using ExpectedArtifact = expected::expected<Artifact, error::Error>;

ExpectedArtifact Parse(io::Reader &reader, config::ParserConfig conf = {});

} // namespace parser
} // namespace artifact

} // namespace mender
#endif // MENDER_ARTIFACT_PARSER_HPP
