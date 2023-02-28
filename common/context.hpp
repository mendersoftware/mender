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

#ifndef MENDER_COMMON_CONTEXT_HPP
#define MENDER_COMMON_CONTEXT_HPP

#include <common/conf.hpp>
#include <common/error.hpp>
#include <common/key_value_database.hpp>

#if MENDER_USE_LMDB
#include <common/key_value_database_lmdb.hpp>
#else
#error MenderContext requires LMDB
#endif // MENDER_USE_LMDB

namespace mender {
namespace common {
namespace context {

namespace conf = mender::common::conf;
namespace error = mender::common::error;
namespace kv_db = mender::common::key_value_database;

class MenderContext {
public:
	error::Error Initialize(const conf::MenderConfig &config);
	kv_db::KeyValueDatabase &GetMenderStoreDB();

private:
#if MENDER_USE_LMDB
	kv_db::KeyValueDatabaseLmdb mender_store_;
#endif // MENDER_USE_LMDB
};

} // namespace context
} // namespace common
} // namespace mender

#endif // MENDER_COMMON_CONTEXT_HPP
