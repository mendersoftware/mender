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

#include <common/context.hpp>

#include <common/error.hpp>
#include <common/conf/paths.hpp>

namespace mender {
namespace common {
namespace context {

using namespace std;
namespace conf = mender::common::conf;
namespace error = mender::common::error;

error::Error MenderContext::Initialize(const conf::MenderConfig &config) {
#if MENDER_USE_LMDB
	auto err = mender_store_.Open(conf::paths::Join(config.data_store_dir, "mender-store"));
	if (error::NoError != err) {
		return err;
	}
	err = mender_store_.Remove(AuthTokenName);
	if (error::NoError != err) {
		// key not existing in the DB is not treated as an error so this must be
		// a real error
		return err;
	}
	err = mender_store_.Remove(AuthTokenCacheInvalidatorName);
	if (error::NoError != err) {
		// same as above -- a real error
		return err;
	}

	return error::NoError;
#else
	return error::NoError;
#endif
}

kv_db::KeyValueDatabase &MenderContext::GetMenderStoreDB() {
	return mender_store_;
}

} // namespace context
} // namespace common
} // namespace mender
