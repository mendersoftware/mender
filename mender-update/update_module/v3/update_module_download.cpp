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

#include <mender-update/update_module/v3/update_module.hpp>

#include <mender-update/progress_reader/progress_reader.hpp>

#include <common/events.hpp>
#include <common/events_io.hpp>
#include <common/log.hpp>
#include <common/path.hpp>
#include <common/processes.hpp>

namespace mender {
namespace update {
namespace update_module {
namespace v3 {

namespace log = mender::common::log;
namespace path = mender::common::path;
namespace processes = mender::common::processes;
namespace progress = mender::update::progress;


void UpdateModule::StartDownloadProcess() {
	log::Debug(
		"Calling Update Module with command `" + update_module_path_ + " Download "
		+ update_module_workdir_ + "`.");
	download_->proc_ = make_shared<procs::Process>(
		vector<string> {update_module_path_, "Download", update_module_workdir_});

	download_->proc_->SetWorkDir(update_module_workdir_);

	auto err = PrepareStreamNextPipe();
	if (err != error::NoError) {
		DownloadErrorHandler(err);
		return;
	}

	processes::OutputHandler stdout_handler {"Update Module output (stdout): "};
	processes::OutputHandler stderr_handler {"Update Module output (stderr): "};

	err = download_->proc_->Start(stdout_handler, stderr_handler);
	if (err != error::NoError) {
		DownloadErrorHandler(GetProcessError(err));
		return;
	}

	err = download_->proc_->AsyncWait(
		download_->event_loop_,
		[this](error::Error err) {
			if (err.code == make_error_condition(errc::timed_out)) {
				DownloadTimeoutHandler();
			} else {
				ProcessEndedHandler(err);
			}
		},
		chrono::seconds(ctx_.GetConfig().module_timeout_seconds));
	if (err != error::NoError) {
		DownloadErrorHandler(err);
		return;
	}

	DownloadErrorHandler(OpenStreamNextPipe(
		[this](io::ExpectedAsyncWriterPtr writer) { StreamNextOpenHandler(writer); }));
}

void UpdateModule::StreamNextOpenHandler(io::ExpectedAsyncWriterPtr writer) {
	if (!writer) {
		DownloadErrorHandler(writer.error());
		return;
	}
	download_->stream_next_writer_ = writer.value();

	download_->module_has_started_download_ = true;

	auto reader = download_->payload_.Next();
	if (!reader) {
		if (reader.error().code
			== artifact::parser_error::MakeError(
				   artifact::parser_error::NoMorePayloadFilesError, "")
				   .code) {
			download_->module_has_finished_download_ = true;
			log::Debug("Update Module finished all downloads");
			EndStreamNext();
		} else {
			DownloadErrorHandler(reader.error());
		}
		return;
	}
	auto payload_reader = make_shared<artifact::Reader>(std::move(reader.value()));

	auto progress_reader = make_shared<progress::Reader>(payload_reader, payload_reader->Size());

	download_->current_payload_reader_ =
		make_shared<events::io::AsyncReaderFromReader>(download_->event_loop_, progress_reader);
	download_->current_payload_name_ = payload_reader->Name();

	auto stream_path =
		path::Join(update_module_workdir_, string("streams"), download_->current_payload_name_);
	DownloadErrorHandler(PrepareAndOpenStreamPipe(
		stream_path, [this](io::ExpectedAsyncWriterPtr writer) { StreamOpenHandler(writer); }));

	string stream_next_string = path::Join("streams", download_->current_payload_name_);
	size_t entry_size = stream_next_string.size() + 1;
	if (entry_size > download_->buffer_.size()) {
		DownloadErrorHandler(error::Error(
			make_error_condition(errc::no_buffer_space), "Payload name is too large for buffer"));
		return;
	}
	copy(stream_next_string.begin(), stream_next_string.end(), download_->buffer_.begin());
	download_->buffer_[entry_size - 1] = '\n';
	DownloadErrorHandler(download_->stream_next_writer_->AsyncWrite(
		download_->buffer_.begin(),
		download_->buffer_.begin() + entry_size,
		[this, entry_size](io::ExpectedSize result) {
			StreamNextWriteHandler(entry_size, result);
		}));
}

void UpdateModule::StreamOpenHandler(io::ExpectedAsyncWriterPtr writer) {
	if (!writer) {
		DownloadErrorHandler(writer.error());
		return;
	}
	download_->current_stream_writer_ = writer.value();

	DownloadErrorHandler(download_->current_payload_reader_->AsyncRead(
		download_->buffer_.begin(), download_->buffer_.end(), [this](io::ExpectedSize result) {
			PayloadReadHandler(result);
		}));
}

void UpdateModule::StreamNextWriteHandler(size_t expected_n, io::ExpectedSize result) {
	// Close stream-next writer.
	download_->stream_next_writer_.reset();
	if (!result) {
		DownloadErrorHandler(result.error());
	} else if (expected_n != result.value()) {
		DownloadErrorHandler(error::Error(
			make_error_condition(errc::io_error),
			"Unexpected number of written bytes to stream-next"));
	}
}

void UpdateModule::PayloadReadHandler(io::ExpectedSize result) {
	if (!result) {
		// Close streams.
		download_->current_stream_writer_.reset();
		download_->current_payload_reader_.reset();
		DownloadErrorHandler(result.error());
	} else if (result.value() > 0) {
		DownloadErrorHandler(download_->current_stream_writer_->AsyncWrite(
			download_->buffer_.begin(),
			download_->buffer_.begin() + result.value(),
			[this, result](io::ExpectedSize write_result) {
				StreamWriteHandler(result.value(), write_result);
			}));
	} else {
		// Close streams.
		download_->current_stream_writer_.reset();
		download_->current_payload_reader_.reset();

		if (download_->downloading_to_files_) {
			StartDownloadToFile();
		} else {
			DownloadErrorHandler(OpenStreamNextPipe(
				[this](io::ExpectedAsyncWriterPtr writer) { StreamNextOpenHandler(writer); }));
		}
	}
}

void UpdateModule::StreamWriteHandler(size_t expected_n, io::ExpectedSize result) {
	if (!result) {
		DownloadErrorHandler(result.error());
	} else if (expected_n != result.value()) {
		DownloadErrorHandler(error::Error(
			make_error_condition(errc::io_error),
			"Unexpected number of written bytes to download stream"));
	} else {
		download_->written_ += result.value();
		log::Trace("Wrote " + to_string(download_->written_) + " bytes to Update Module");
		DownloadErrorHandler(download_->current_payload_reader_->AsyncRead(
			download_->buffer_.begin(), download_->buffer_.end(), [this](io::ExpectedSize result) {
				PayloadReadHandler(result);
			}));
	}
}

void UpdateModule::EndStreamNext() {
	// Empty write.
	DownloadErrorHandler(download_->stream_next_writer_->AsyncWrite(
		download_->buffer_.begin(), download_->buffer_.begin(), [this](io::ExpectedSize result) {
			if (!result) {
				DownloadErrorHandler(result.error());
			} else {
				DownloadErrorHandler(error::NoError);
			}
			// Close writer.
			download_->stream_next_writer_.reset();
			// No further action necessary. Now we just need to wait for the process to finish.
		}));
}

void UpdateModule::DownloadErrorHandler(const error::Error &err) {
	if (err != error::NoError) {
		EndDownloadLoop(err);
	}
}

void UpdateModule::EndDownloadLoop(const error::Error &err) {
	download_->download_finished_handler_(err);
}

void UpdateModule::DownloadTimeoutHandler() {
	download_->proc_->EnsureTerminated();
	EndDownloadLoop(error::Error(
		make_error_condition(errc::timed_out), "Update Module Download process timed out"));
}

void UpdateModule::ProcessEndedHandler(error::Error err) {
	if (err != error::NoError) {
		err = GetProcessError(err);
		DownloadErrorHandler(error::Error(
			err.code, "Download: Update Module returned non-zero status: " + err.message));
	} else if (download_->module_has_finished_download_) {
		EndDownloadLoop(error::NoError);
	} else if (download_->module_has_started_download_) {
		DownloadErrorHandler(error::Error(
			make_error_condition(errc::broken_pipe),
			"Update Module started downloading, but did not finish"));
	} else {
		download_->downloading_to_files_ = true;
		download_->stream_next_opener_.reset();
		download_->current_stream_opener_.reset();
		err = DeleteStreamsFiles();
		if (err != error::NoError) {
			DownloadErrorHandler(err);
		} else {
			StartDownloadToFile();
		}
	}
}

void UpdateModule::StartDownloadToFile() {
	auto reader = download_->payload_.Next();
	if (!reader) {
		if (reader.error().code
			== artifact::parser_error::MakeError(
				   artifact::parser_error::NoMorePayloadFilesError, "")
				   .code) {
			log::Debug("Downloaded all files to `files` directory.");
			EndDownloadLoop(error::NoError);
		} else {
			DownloadErrorHandler(reader.error());
		}
		return;
	}
	auto payload_reader = make_shared<artifact::Reader>(std::move(reader.value()));
	download_->current_payload_reader_ =
		make_shared<events::io::AsyncReaderFromReader>(download_->event_loop_, payload_reader);
	download_->current_payload_name_ = payload_reader->Name();

	auto stream_path = path::Join(update_module_workdir_, string("files"));
	auto err = PrepareDownloadDirectory(stream_path);
	if (err != error::NoError) {
		DownloadErrorHandler(err);
		return;
	}

	stream_path = path::Join(stream_path, download_->current_payload_name_);

	auto current_stream_writer =
		make_shared<events::io::AsyncFileDescriptorWriter>(download_->event_loop_);
	err = current_stream_writer->Open(stream_path);
	if (err != error::NoError) {
		DownloadErrorHandler(err);
		return;
	}
	download_->current_stream_writer_ = current_stream_writer;

	DownloadErrorHandler(download_->current_payload_reader_->AsyncRead(
		download_->buffer_.begin(), download_->buffer_.end(), [this](io::ExpectedSize result) {
			PayloadReadHandler(result);
		}));
}

} // namespace v3
} // namespace update_module
} // namespace update
} // namespace mender
