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

#ifndef MENDER_ARTIFACT_LEXER_HPP
#define MENDER_ARTIFACT_LEXER_HPP

#include <cstdint>
#include <unordered_map>

#include <system_error>

#include <common/io.hpp>
#include <common/log.hpp>
#include <common/expected.hpp>
#include <artifact/tar/tar.hpp>

#include <artifact/sha/sha.hpp>

#include <artifact/token.hpp>


namespace mender {
namespace artifact {
namespace lexer {

using namespace std;

namespace log = mender::common::log;

class Lexer {
private:
	std::shared_ptr<mender::tar::Reader> tar_reader_;

public:
	token::Token current;

	Lexer(std::shared_ptr<mender::tar::Reader> tr) :
		tar_reader_ {tr},
		current {} {
	}

	token::Token &Next() {
		auto entry = tar_reader_->Next();
		if (!entry) {
			log::Error("Error reading the next tar entry: " + entry.error().message);
			this->current = token::Token {token::Type::Unrecognized};
			return this->current;
		}
		log::Trace("Entry name: " + entry.value().Name());
		this->current = token::Token {entry.value().Name(), entry.value()};
		return current;
	}
};

} // namespace lexer
} // namespace artifact
} // namespace mender

#endif // MENDER_ARTIFACT_LEXER_HPP
