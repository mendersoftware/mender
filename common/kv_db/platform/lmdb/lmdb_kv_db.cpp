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

#include <iostream>
#include <lmdb++.h>
#include <cstdio>

#include <common/kv_db.hpp>

namespace kv_db {

void KeyValueDB::hello_world() {
	/* Create and open the LMDB environment: */
	auto env = lmdb::env::create();
	env.set_mapsize(1024UL * 1024UL); /* 1 MiB */
	env.open("./hello_world.lmdb", MDB_NOSUBDIR, 0664);
	lmdb::dbi dbi;

	// Get the dbi handle, and insert some key/value pairs in a write transaction:
	{
		auto wtxn = lmdb::txn::begin(env);
		dbi = lmdb::dbi::open(wtxn, nullptr);
		dbi.put(wtxn, "hello", "world");
		wtxn.commit();
	}

	// In a read-only transaction, get and print one of the values:
	{
		auto rtxn = lmdb::txn::begin(env, nullptr, MDB_RDONLY);
		std::string_view world;
		if (dbi.get(rtxn, "hello", world)) {
			std::cout << "The value of 'hello' in the DB is: '" << world << "'" << std::endl;
		} else {
			std::cout << "The value for 'hello' not found in the DB!" << std::endl;
		}
	} // rtxn aborted automatically

	std::remove("hello_world.lmdb");
	std::remove("hello_world.lmdb-lock");
}

} // namespace kv_db
