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

#include <common/log.hpp>


#include <boost/smart_ptr/shared_ptr.hpp>
#include <boost/core/null_deleter.hpp>
#include <boost/date_time/posix_time/posix_time.hpp>
#include <boost/log/common.hpp>
#include <boost/log/expressions.hpp>
#include <boost/log/attributes.hpp>
#include <boost/log/sinks.hpp>
#include <boost/log/sources/logger.hpp>
#include <boost/log/utility/manipulators/add_value.hpp>
#include <boost/log/attributes/scoped_attribute.hpp>
#include <boost/log/support/date_time.hpp>

#include <string>
#include <fstream>
#include <common/error.hpp>
#include <common/expected.hpp>

namespace mender {
namespace common {
namespace log {

namespace logging = boost::log;
namespace expr = boost::log::expressions;
namespace sinks = boost::log::sinks;
namespace attrs = boost::log::attributes;
namespace src = boost::log::sources;

namespace error = mender::common::error;
namespace expected = mender::common::expected;

using namespace std;


const LogErrorCategoryClass LogErrorCategory;

const char *LogErrorCategoryClass::name() const noexcept {
	return "LogErrorCategory";
}

string LogErrorCategoryClass::message(int code) const {
	switch (code) {
	case NoError:
		return "Success";
	case InvalidLogLevelError:
		return "Invalid log level given";
	case LogFileError:
		return "Bad log file";
	default:
		return "Unknown";
	}
}

error::Error MakeError(LogErrorCode code, const string &msg) {
	return error::Error(error_condition(code, LogErrorCategory), msg);
}

ExpectedLogLevel StringToLogLevel(const string &level_str) {
	if (level_str == "fatal") {
		return ExpectedLogLevel(LogLevel::Fatal);
	} else if (level_str == "error") {
		return ExpectedLogLevel(LogLevel::Error);
	} else if (level_str == "warning") {
		return ExpectedLogLevel(LogLevel::Warning);
	} else if (level_str == "info") {
		return ExpectedLogLevel(LogLevel::Info);
	} else if (level_str == "debug") {
		return ExpectedLogLevel(LogLevel::Debug);
	} else if (level_str == "trace") {
		return ExpectedLogLevel(LogLevel::Trace);
	} else {
		return ExpectedLogLevel(expected::unexpected(MakeError(
			LogErrorCode::InvalidLogLevelError, "'" + level_str + "' is not a valid log level")));
	}
}

static void LogfmtFormatter(logging::record_view const &rec, logging::formatting_ostream &strm) {
	strm << "record_id=" << logging::extract<unsigned int>("RecordID", rec) << " ";

	auto level = logging::extract<LogLevel>("Severity", rec);
	if (level) {
		std::string lvl = ToStringLogLevel(level.get());
		strm << "severity=" << lvl << " ";
	}

	auto val = logging::extract<boost::posix_time::ptime>("TimeStamp", rec);
	if (val) {
		strm << "time=\"" << val.get() << "\" ";
	}

	auto name = logging::extract<std::string>("Name", rec);
	if (name) {
		strm << "name=\"" << name.get() << "\" ";
	}

	for (auto f : rec.attribute_values()) {
		auto field = logging::extract<LogField>(f.first.string(), rec);
		if (field) {
			strm << field.get().key << "=\"" << field.get().value << "\" ";
		}
	}

	strm << "msg=\"" << rec[expr::smessage] << "\" ";
}

static void SetupLoggerSinks() {
	typedef sinks::synchronous_sink<sinks::text_ostream_backend> text_sink;
	boost::shared_ptr<text_sink> sink(new text_sink);

	{
		text_sink::locked_backend_ptr pBackend = sink->locked_backend();
		boost::shared_ptr<std::ostream> pStream(&std::clog, boost::null_deleter());
		pBackend->add_stream(pStream);
	}

	sink->set_formatter(&LogfmtFormatter);

	logging::core::get()->add_sink(sink);
}

static void SetupLoggerAttributes() {
	attrs::counter<unsigned int> RecordID(1);
	logging::core::get()->add_global_attribute("RecordID", RecordID);

	attrs::local_clock TimeStamp;
	logging::core::get()->add_global_attribute("TimeStamp", TimeStamp);
}

Logger::Logger(const string &name) :
	Logger {name, global_logger_.Level()} {
}

Logger::Logger(const string &name, LogLevel level) :
	name_ {name},
	level_ {level} {
	src::severity_logger<LogLevel> slg;
	slg.add_attribute("Name", attrs::constant<std::string>(name));
	this->logger = slg;
}

void Logger::Log_(LogLevel level, const string &message) {
	BOOST_LOG_SEV(this->logger, level) << message;
}

void Logger::SetLevel(LogLevel level) {
	this->level_ = level;
}

LogLevel Logger::Level() {
	return this->level_;
}

void Logger::AddField(const LogField &field) {
	this->logger.add_attribute(field.key, attrs::constant<LogField>(field));
	return;
}

Logger Setup() {
	SetupLoggerSinks();
	SetupLoggerAttributes();
#ifdef NDEBUG
	return Logger("Global", LogLevel::Info);
#else
	return Logger("Global", LogLevel::Debug);
#endif
}

Logger global_logger_ = Setup();

void SetLevel(LogLevel level) {
	global_logger_.SetLevel(level);
}

error::Error SetupFileLogging(const string &log_file_path, bool exclusive) {
	typedef sinks::synchronous_sink<sinks::text_ostream_backend> text_sink;
	boost::shared_ptr<text_sink> sink = boost::make_shared<text_sink>();

	// Add a stream to write log to
	auto log_stream = boost::make_shared<std::ofstream>();
	errno = 0;
	log_stream->open(log_file_path);
	if (!(*log_stream.get())) {
		auto io_errno = errno;
		return MakeError(
			LogErrorCode::LogFileError,
			"Failed to open '" + log_file_path + "' for logging: " + strerror(io_errno));
	}
	sink->set_formatter(&LogfmtFormatter);

	sink->locked_backend()->add_stream(log_stream);
	sink->locked_backend()->auto_flush(true);

	if (exclusive) {
		logging::core::get()->remove_all_sinks();
	}

	// Register the sink in the logging core
	logging::core::get()->add_sink(sink);

	return error::NoError;
}

LogLevel Level() {
	return global_logger_.Level();
}

void Log_(LogLevel level, const string message) {
	global_logger_.Log(level, message);
}

void Fatal(const string &message) {
	return global_logger_.Log(LogLevel::Fatal, message);
}
void Error(const string &message) {
	return global_logger_.Log(LogLevel::Error, message);
}
void Warning(const string &message) {
	return global_logger_.Log(LogLevel::Warning, message);
}
void Info(const string &message) {
	return global_logger_.Log(LogLevel::Info, message);
}
void Debug(const string &message) {
	return global_logger_.Log(LogLevel::Debug, message);
}
void Trace(const string &message) {
	return global_logger_.Log(LogLevel::Trace, message);
}


} // namespace log
} // namespace common
} // namespace mender
