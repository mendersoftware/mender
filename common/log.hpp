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

#ifndef MENDER_LOG_HPP
#define MENDER_LOG_HPP

#include <config.h>

#ifdef MENDER_LOG_BOOST
#include <boost/log/common.hpp>
#include <boost/log/sources/logger.hpp>
#endif

#include <string>
#include <cassert>

namespace mender::common::log {

using namespace std;

struct LogField {
	LogField(const string &key, const string &value) :
		key(key),
		value(value) {
	}

	string key;
	string value;
};


enum class LogLevel {
	Fatal = 0,
	Error = 1,
	Warning = 2,
	Info = 3,
	Debug = 4,
	Trace = 5,
};

inline string to_string_level(LogLevel lvl) {
	switch (lvl) {
	case LogLevel::Fatal:
		return "fatal";
	case LogLevel::Error:
		return "error";
	case LogLevel::Warning:
		return "warning";
	case LogLevel::Info:
		return "info";
	case LogLevel::Debug:
		return "debug";
	case LogLevel::Trace:
		return "trace";
	}
	assert(false);
}


void Setup();

class Logger {
private:
#ifdef MENDER_LOG_BOOST
	boost::log::sources::severity_logger<LogLevel> logger;
#endif

	LogLevel level_ {LogLevel::Info};

	string name_ {};

	void AddField(const LogField &field);

public:
	explicit Logger(const string &name);
	Logger(const string &name, LogLevel level);

	void SetLevel(LogLevel level);

	LogLevel Level();

	template <typename... Fields>
	Logger WithFields(const Fields &...fields) {
		auto l = Logger(this->name_);
		l.SetLevel(this->level_);
		for (const auto f : {fields...}) {
			l.AddField(f);
		}
		return l;
	}

	void Log(LogLevel level, const string &message);

	void Fatal(const string &message) {
		Log(LogLevel::Fatal, message);
	}

	void Error(const string &message) {
		Log(LogLevel::Error, message);
	}

	void Warning(const string &message) {
		Log(LogLevel::Warning, message);
	}

	void Info(const string &message) {
		Log(LogLevel::Info, message);
	}

	void Debug(const string &message) {
		Log(LogLevel::Debug, message);
	}

	void Trace(const string &message) {
		Log(LogLevel::Trace, message);
	}
};


} // namespace mender::common::log


// Add a global logger to the namespace
namespace mender::common::log {

extern Logger global_logger_;

void SetLevel(LogLevel level);

LogLevel Level();

template <typename... Fields>
Logger WithFields(const Fields &...fields) {
	return global_logger_.WithFields(fields...);
}

void Log(LogLevel level, const string &message);
void Fatal(const string &message);
void Error(const string &message);
void Warning(const string &message);
void Info(const string &message);
void Debug(const string &message);
void Trace(const string &message);

} // namespace mender::common::log

#endif // MENDER_LOG_HPP
