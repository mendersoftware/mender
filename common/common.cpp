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

#include <common/common.hpp>
#include <common/error.hpp>

#include <cerrno>
#include <cstdlib>

namespace mender {
namespace common {

mender::common::expected::ExpectedLongLong StringToLongLong(const string &str, int base) {
	char *end;
	errno = 0;
	long long num = strtoll(str.c_str(), &end, base);
	if (errno != 0) {
		int int_error = errno;
		return expected::unexpected(mender::common::error::Error(
			std::generic_category().default_error_condition(int_error), ""));
	}
	if (end != &*str.end()) {
		return expected::unexpected(mender::common::error::Error(
			std::make_error_condition(errc::invalid_argument),
			str + " had trailing non-numeric data"));
	}

	return num;
}

vector<string> SplitString(const string &str, const string &delim) {
	vector<string> ret;
	for (size_t begin = 0, end = str.find(delim);;) {
		if (end == string::npos) {
			ret.push_back(string(str.begin() + begin, str.end()));
			break;
		} else {
			ret.push_back(string(str.begin() + begin, str.begin() + end));
			begin = end + 1;
			end = str.find(delim, begin);
		}
	}
	return ret;
}

string JoinStrings(const vector<string> &str, const string &delim) {
	string ret;
	auto s = str.begin();
	if (s == str.end()) {
		return ret;
	}
	ret += *s;
	s++;
	for (; s != str.end(); s++) {
		ret += delim;
		ret += *s;
	}
	return ret;
}

vector<string> JoinStringsMaxWidth(
	const vector<string> &str, const string &delim, const size_t max_width) {
	vector<string> ret;
	auto s = str.begin();
	if (s == str.end()) {
		return ret;
	}

	string line = *s;
	s++;
	for (; s != str.end(); s++) {
		if (line.size() + delim.size() + (*s).size() > max_width) {
			ret.push_back(line);
			line = "";
		} else {
			line += delim;
		}
		line += *s;
	}
	ret.push_back(line);
	return ret;
}

} // namespace common
} // namespace mender
