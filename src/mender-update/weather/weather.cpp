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

#include <mender-update/weather/weather.hpp>

#include <common/log.hpp>
#include <common/json.hpp>
#include <common/io.hpp>

namespace mender {
namespace update {
namespace weather {

namespace log = mender::common::log;
namespace json = mender::common::json;
namespace io = mender::common::io;

WeatherChecker::WeatherChecker(
	const string &api_key, const string &location, events::EventLoop &event_loop) :
	api_key_(api_key),
	location_(location),
	event_loop_(event_loop),
	http_client_(http::ClientConfig {}, event_loop, "weather_client") {
}

void WeatherChecker::AsyncCheckUpdateConditions(CheckWeatherHandler handler) {
	log::Info("Performing weather-based update safety assessment...");

	// Build OpenWeatherMap API URL
	string url = "https://api.openweathermap.org/data/2.5/weather?q="
				 + http::URLEncode(location_) + "&appid=" + api_key_ + "&units=metric";

	auto req = make_shared<http::OutgoingRequest>();
	auto err = req->SetAddress(url);
	if (err != error::NoError) {
		log::Warning("Failed to set weather API address: " + err.String());
		log::Warning("Proceeding with update without weather check");
		handler(expected::ExpectedBool(true));
		return;
	}

	req->SetMethod(http::Method::GET);
	req->SetHeader("User-Agent", "Mender/4.0");

	log::Debug("Fetching weather data from: " + url);

	// Async HTTP request using existing Mender http::Client
	auto err_call = http_client_.AsyncCall(
		req,
		[this, handler](http::ExpectedIncomingResponsePtr exp_resp) {
			this->HandleWeatherResponse(exp_resp, handler);
		},
		[handler](http::ExpectedIncomingResponsePtr exp_resp) {
			// Body handler - not needed for this use case
		});

	if (err_call != error::NoError) {
		log::Warning("Failed to initiate weather check: " + err_call.String());
		log::Warning("Proceeding with update without weather check");
		handler(expected::ExpectedBool(true));
	}
}

void WeatherChecker::HandleWeatherResponse(
	http::ExpectedIncomingResponsePtr exp_resp, CheckWeatherHandler handler) {
	if (!exp_resp) {
		log::Warning("Weather API request failed: " + exp_resp.error().String());
		log::Warning("Proceeding with update without weather check");
		handler(expected::ExpectedBool(true));
		return;
	}

	auto resp = exp_resp.value();
	auto status = resp->GetStatusCode();

	if (status != http::StatusOK) {
		log::Warning("Weather API returned status " + to_string(status));
		log::Warning("Proceeding with update without weather check");
		handler(expected::ExpectedBool(true));
		return;
	}

	// Read response body using existing I/O infrastructure
	auto body_reader = resp->MakeBodyAsyncReader();
	if (!body_reader) {
		log::Warning("Failed to read weather response body");
		log::Warning("Proceeding with update without weather check");
		handler(expected::ExpectedBool(true));
		return;
	}

	// Read into a buffer
	auto buffer = make_shared<vector<uint8_t>>();
	auto byte_writer = make_shared<io::ByteWriter>(buffer);
	byte_writer->SetUnlimited(true);

	io::AsyncCopy(
		byte_writer,
		body_reader.value(),
		[this, buffer, handler](error::Error err) {
			if (err != error::NoError) {
				log::Warning("Failed to read weather response: " + err.String());
				log::Warning("Proceeding with update without weather check");
				handler(expected::ExpectedBool(true));
				return;
			}

			// Convert buffer to string
			string json_str(buffer->begin(), buffer->end());

			// Parse JSON response using existing JSON library
			auto weather_result = this->ParseWeatherResponse(json_str);
			if (!weather_result) {
				log::Warning(
					"Failed to parse weather data: " + weather_result.error().String());
				log::Warning("Proceeding with update without weather check");
				handler(expected::ExpectedBool(true));
				return;
			}

			WeatherConditions weather = weather_result.value();

			log::Info("Weather conditions retrieved:");
			log::Info("  Condition: " + weather.condition + " (" + weather.description + ")");
			log::Info("  Temperature: " + to_string(weather.temperature_celsius) + "°C");
			log::Info("  Humidity: " + to_string(weather.humidity_percent) + "%");
			log::Info("  Wind speed: " + to_string(weather.wind_speed_kmh) + " km/h");

			// Evaluate conditions
			auto err_eval = this->EvaluateConditions(weather);
			if (err_eval != error::NoError) {
				handler(expected::unexpected(err_eval));
			} else {
				handler(expected::ExpectedBool(true));
			}
		});
}

expected::Expected<WeatherConditions> WeatherChecker::ParseWeatherResponse(
	const string &json_str) {
	WeatherConditions conditions;

	auto json_data = json::Load(json_str);

	if (!json_data) {
		return expected::unexpected(error::MakeError(
			error::GenericError, "Failed to parse JSON: " + json_data.error().String()));
	}

	auto data = json_data.value();

	// Parse main weather data
	auto main = data.Get("main");
	if (!main) {
		return expected::unexpected(
			error::MakeError(error::GenericError, "Missing 'main' field in weather response"));
	}

	auto temp = main.value().Get("temp");
	auto humidity = main.value().Get("humidity");

	if (temp) {
		auto temp_val = temp.value().GetDouble();
		if (temp_val) {
			conditions.temperature_celsius = static_cast<float>(temp_val.value());
		}
	}
	if (humidity) {
		auto hum_val = humidity.value().GetInt64();
		if (hum_val) {
			conditions.humidity_percent = static_cast<int>(hum_val.value());
		}
	}

	// Parse wind data
	auto wind = data.Get("wind");
	if (wind) {
		auto speed = wind.value().Get("speed");
		if (speed) {
			auto speed_val = speed.value().GetDouble();
			if (speed_val) {
				// Convert m/s to km/h
				conditions.wind_speed_kmh = static_cast<float>(speed_val.value() * 3.6);
			}
		}
	}

	// Parse weather condition array
	auto weather_array = data.Get("weather");
	if (weather_array) {
		auto array_size = weather_array.value().GetArraySize();
		if (array_size && array_size.value() > 0) {
			auto first_weather = weather_array.value().Get(static_cast<size_t>(0));
			if (first_weather) {
				auto main_cond = first_weather.value().Get("main");
				auto desc = first_weather.value().Get("description");

				if (main_cond) {
					auto cond_str = main_cond.value().GetString();
					if (cond_str) {
						conditions.condition = cond_str.value();
					}
				}
				if (desc) {
					auto desc_str = desc.value().GetString();
					if (desc_str) {
						conditions.description = desc_str.value();
					}
				}
			}
		}
	}

	return conditions;
}

error::Error WeatherChecker::EvaluateConditions(const WeatherConditions &weather) {
	// Check for dangerous weather conditions
	if (weather.condition == "Thunderstorm") {
		log::Error("UNSAFE CONDITIONS: Thunderstorm detected");
		log::Error("  Risk: Power instability, lightning strikes, electrical surges");
		log::Error("  Recommendation: Defer update for 2-4 hours");
		return error::MakeError(
			error::GenericError, "Thunderstorm detected - update deferred for safety");
	}

	if (weather.condition == "Tornado" || weather.condition == "Hurricane") {
		log::Error("CRITICAL: Severe weather event detected");
		log::Error("  Deferring all update operations until conditions improve");
		return error::MakeError(error::GenericError, "Severe weather - updates suspended");
	}

	// Temperature range check
	if (weather.temperature_celsius > 35.0) {
		log::Warning(
			"SUBOPTIMAL: High temperature detected (" + to_string(weather.temperature_celsius)
			+ "°C)");
		log::Warning("  Risk: CPU thermal throttling during artifact verification");
		log::Warning("  Risk: Increased flash memory write errors");
		log::Warning("  Recommendation: Wait for cooler conditions");
		return error::MakeError(
			error::GenericError, "Temperature too high for optimal update conditions");
	}

	if (weather.temperature_celsius < 0.0) {
		log::Warning(
			"SUBOPTIMAL: Low temperature detected (" + to_string(weather.temperature_celsius)
			+ "°C)");
		log::Warning("  Risk: Flash memory write reliability degraded below 0°C");
		log::Warning("  Recommendation: Wait for warmer conditions");
		return error::MakeError(
			error::GenericError, "Temperature too low for reliable flash operations");
	}

	// Humidity check
	if (weather.humidity_percent > 85) {
		log::Warning("HIGH HUMIDITY: " + to_string(weather.humidity_percent) + "%");
		log::Warning("  Risk: Condensation affecting electronic components");
		log::Info("  Proceeding with caution...");
	}

	// All clear!
	log::Info("✓ Weather conditions optimal for update operations");
	log::Info(
		"  Temperature: " + to_string(weather.temperature_celsius)
		+ "°C (optimal range: 0-35°C)");
	log::Info("  Conditions: " + weather.condition + " (safe for updates)");
	log::Info("  Humidity: " + to_string(weather.humidity_percent) + "% (acceptable)");

	return error::NoError;
}

} // namespace weather
} // namespace update
} // namespace mender
