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

#ifndef MENDER_UPDATE_WEATHER_WEATHER_HPP
#define MENDER_UPDATE_WEATHER_WEATHER_HPP

#include <common/error.hpp>
#include <common/expected.hpp>
#include <common/http.hpp>
#include <common/events.hpp>

#include <string>
#include <functional>

namespace mender {
namespace update {
namespace weather {

namespace error = mender::common::error;
namespace expected = mender::common::expected;
namespace http = mender::common::http;
namespace events = mender::common::events;

using namespace std;

struct WeatherConditions {
	string condition;      // "clear", "rain", "thunderstorm", etc.
	float temperature_celsius;
	int humidity_percent;
	float wind_speed_kmh;
	string description;
};

using CheckWeatherHandler = function<void(expected::ExpectedBool)>;

class WeatherChecker {
public:
	WeatherChecker(
		const string &api_key,
		const string &location,
		events::EventLoop &event_loop);

	// Async check if conditions are suitable for updates
	void AsyncCheckUpdateConditions(CheckWeatherHandler handler);

private:
	string api_key_;
	string location_;
	events::EventLoop &event_loop_;
	http::Client http_client_;

	void HandleWeatherResponse(
		http::ExpectedIncomingResponsePtr exp_resp,
		CheckWeatherHandler handler);
	expected::Expected<WeatherConditions> ParseWeatherResponse(const string &json_str);
	error::Error EvaluateConditions(const WeatherConditions &weather);
};

} // namespace weather
} // namespace update
} // namespace mender

#endif // MENDER_UPDATE_WEATHER_WEATHER_HPP
