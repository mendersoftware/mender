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

#ifndef MENDER_AUTH_CONTEXT_HPP
#define MENDER_AUTH_CONTEXT_HPP

#include <common/conf.hpp>
#include <common/error.hpp>

namespace mender {
namespace auth {
namespace context {

namespace conf = mender::common::conf;
namespace error = mender::common::error;

using namespace std;

enum MenderContextErrorCode {
	NoError = 0,
};

class MenderContextErrorCategoryClass : public std::error_category {
public:
	const char *name() const noexcept override;
	string message(int code) const override;
};
extern const MenderContextErrorCategoryClass MenderContextErrorCategory;

error::Error MakeError(MenderContextErrorCode code, const string &msg);

class MenderContext {
public:
	MenderContext(conf::MenderConfig &config) :
		config_ {config} {};
	virtual ~MenderContext() {
	}

	error::Error Initialize();
	const conf::MenderConfig &GetConfig() const {
		return config_;
	}

private:
	conf::MenderConfig &config_;
};

} // namespace context
} // namespace auth
} // namespace mender

#endif // MENDER_AUTH_CONTEXT_HPP
