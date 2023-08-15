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

#ifndef MENDER_UPDATE_INVENTORY_HPP
#define MENDER_UPDATE_INVENTORY_HPP

#include <string>

#include <common/error.hpp>
#include <common/events.hpp>
#include <common/expected.hpp>
#include <common/http.hpp>
#include <common/json.hpp>
#include <common/optional.hpp>

namespace mender {
namespace update {
namespace inventory {

using namespace std;

namespace error = mender::common::error;
namespace events = mender::common::events;
namespace expected = mender::common::expected;
namespace json = mender::common::json;
namespace optional = mender::common::optional;

enum InventoryErrorCode {
	NoError = 0,
	BadResponseError,
};
class InventoryErrorCategoryClass : public std::error_category {
public:
	const char *name() const noexcept override;
	string message(int code) const override;
};
extern const InventoryErrorCategoryClass InventoryErrorCategory;

error::Error MakeError(InventoryErrorCode code, const string &msg);

using APIResponse = error::Error;
using APIResponseHandler = function<void(APIResponse)>;

error::Error PushInventoryData(
	const string &inventory_generators_dir,
	const string &server_url,
	events::EventLoop &loop,
	http::Client &client,
	size_t &last_data_hash,
	APIResponseHandler api_handler);

} // namespace inventory
} // namespace update
} // namespace mender

#endif // MENDER_UPDATE_INVENTORY_HPP
