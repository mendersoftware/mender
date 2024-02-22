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

#include <common/events_io.hpp>

#include <vector>
#include <fstream>

#include <gtest/gtest.h>

#include <common/path.hpp>
#include <common/setup.hpp>
#include <common/testing.hpp>

using namespace std;

namespace error = mender::common::error;
namespace events = mender::common::events;
namespace io = mender::common::io;
namespace mtesting = mender::common::testing;
namespace path = mender::common::path;
namespace expected = mender::common::expected;

using TestEventLoop = mtesting::TestEventLoop;

TEST(EventsIo, ReadAndWriteWithPipes) {
	TestEventLoop loop;

	int fds[2];
	ASSERT_EQ(pipe(fds), 0);

	events::io::AsyncFileDescriptorReader reader(loop, fds[0]);
	events::io::AsyncFileDescriptorWriter writer(loop, fds[1]);

	const uint8_t data[] = "abcd";

	vector<uint8_t> to_send(data, data + sizeof(data));
	vector<uint8_t> to_receive;
	to_receive.resize(to_send.size());

	auto err =
		reader.AsyncRead(to_receive.begin(), to_receive.end(), [&loop](io::ExpectedSize result) {
			EXPECT_TRUE(result);
			EXPECT_EQ(result.value(), 5);

			loop.Stop();
		});
	ASSERT_EQ(err, error::NoError);
	err = writer.AsyncWrite(to_send.begin(), to_send.end(), [](io::ExpectedSize result) {
		EXPECT_TRUE(result);
		EXPECT_EQ(result.value(), 5);
	});
	ASSERT_EQ(err, error::NoError);

	loop.Run();

	EXPECT_EQ(to_receive, to_send);
}

TEST(IO, AsyncBufferedReader) {
	TestEventLoop loop;

	int fds[2];
	ASSERT_EQ(pipe(fds), 0);

	events::io::AsyncFileDescriptorReader reader(loop, fds[0]);
	io::AsyncBufferedReader buffered_reader(reader);
	events::io::AsyncFileDescriptorWriter writer(loop, fds[1]);

	const uint8_t data[] = "foobarbaz";

	vector<uint8_t> to_send(data, data + sizeof(data));
	vector<uint8_t> to_receive;
	to_receive.resize(to_send.size());

	// Short read
	auto err = buffered_reader.AsyncRead(
		to_receive.begin(),
		to_receive.begin() + 5,
		[&buffered_reader, &to_receive, &to_send, &loop](io::ExpectedSize result) {
			EXPECT_TRUE(result);
			EXPECT_EQ(result.value(), 5);
			EXPECT_EQ(
				(vector<uint8_t> {to_send.begin(), to_send.begin() + 5}),
				(vector<uint8_t> {to_receive.begin(), to_receive.begin() + 5}));

			// Rewind and attempt a long read - it shall read only the buffered data
			auto ex_bytes_rewind = buffered_reader.StopBufferingAndRewind();
			ASSERT_TRUE(ex_bytes_rewind);
			EXPECT_EQ(5, ex_bytes_rewind.value());
			to_receive.clear();
			to_receive.resize(to_send.size());
			auto err = buffered_reader.AsyncRead(
				to_receive.begin(),
				to_receive.end(),
				[&buffered_reader, &to_receive, &to_send, &loop](io::ExpectedSize result) {
					EXPECT_TRUE(result);
					EXPECT_EQ(result.value(), 5);
					EXPECT_EQ(
						(vector<uint8_t> {to_send.begin(), to_send.begin() + 5}),
						(vector<uint8_t> {to_receive.begin(), to_receive.begin() + 5}));

					// Read the remaining data
					auto err = buffered_reader.AsyncRead(
						to_receive.begin() + 5, to_receive.end(), [&loop](io::ExpectedSize result) {
							EXPECT_TRUE(result);
							EXPECT_EQ(result.value(), 5);
							loop.Stop();
						});
					ASSERT_EQ(err, error::NoError);
				});
			ASSERT_EQ(err, error::NoError);
		});
	ASSERT_EQ(err, error::NoError);
	err = writer.AsyncWrite(to_send.begin(), to_send.end(), [](io::ExpectedSize result) {
		EXPECT_TRUE(result);
		EXPECT_EQ(result.value(), 10);
	});
	ASSERT_EQ(err, error::NoError);

	loop.Run();

	EXPECT_EQ(to_receive, to_send);
}

TEST(EventsIo, PartialRead) {
	TestEventLoop loop;

	int fds[2];
	ASSERT_EQ(pipe(fds), 0);

	events::io::AsyncFileDescriptorReader reader(loop, fds[0]);
	events::io::AsyncFileDescriptorWriter writer(loop, fds[1]);

	const uint8_t data[] = "abcd";

	vector<uint8_t> to_send(data, data + sizeof(data));
	vector<uint8_t> to_receive;
	to_receive.resize(to_send.size());

	auto err = reader.AsyncRead(
		to_receive.begin(),
		to_receive.end() - 2,
		[&loop, &reader, &to_send, &to_receive](io::ExpectedSize result) {
			EXPECT_TRUE(result);
			EXPECT_EQ(result.value(), 3);
			// Not yet.
			EXPECT_NE(to_receive, to_send);

			auto err = reader.AsyncRead(
				to_receive.begin() + result.value(),
				to_receive.end(),
				[&loop](io::ExpectedSize result2) {
					EXPECT_TRUE(result2);
					EXPECT_EQ(result2.value(), 2);

					loop.Stop();
				});
			ASSERT_EQ(err, error::NoError);
		});
	ASSERT_EQ(err, error::NoError);
	err = writer.AsyncWrite(to_send.begin(), to_send.end(), [](io::ExpectedSize result) {
		EXPECT_TRUE(result);
		EXPECT_EQ(result.value(), 5);
	});
	ASSERT_EQ(err, error::NoError);

	loop.Run();

	EXPECT_EQ(to_receive, to_send);
}

TEST(EventsIo, PartialWrite) {
	TestEventLoop loop;

	int fds[2];
	ASSERT_EQ(pipe(fds), 0);

	events::io::AsyncFileDescriptorReader reader(loop, fds[0]);
	events::io::AsyncFileDescriptorWriter writer(loop, fds[1]);

	const uint8_t data[] = "abcd";

	vector<uint8_t> to_send(data, data + sizeof(data));
	vector<uint8_t> to_receive;
	to_receive.resize(to_send.size());

	auto err = reader.AsyncRead(
		to_receive.begin(),
		to_receive.end(),
		[&loop, &reader, &writer, &to_send, &to_receive](io::ExpectedSize result) {
			EXPECT_TRUE(result);
			EXPECT_EQ(result.value(), 3);
			// Not yet.
			EXPECT_NE(to_receive, to_send);

			auto err = reader.AsyncRead(
				to_receive.begin() + result.value(),
				to_receive.end(),
				[&loop](io::ExpectedSize result2) {
					EXPECT_TRUE(result2);
					EXPECT_EQ(result2.value(), 2);

					loop.Stop();
				});
			ASSERT_EQ(err, error::NoError);

			err = writer.AsyncWrite(
				to_send.begin() + result.value(), to_send.end(), [](io::ExpectedSize result2) {
					EXPECT_TRUE(result2);
					EXPECT_EQ(result2.value(), 2);
				});
			ASSERT_EQ(err, error::NoError);
		});
	ASSERT_EQ(err, error::NoError);
	err = writer.AsyncWrite(to_send.begin(), to_send.end() - 2, [](io::ExpectedSize result) {
		EXPECT_TRUE(result);
		EXPECT_EQ(result.value(), 3);
	});
	ASSERT_EQ(err, error::NoError);

	loop.Run();

	EXPECT_EQ(to_receive, to_send);
}

TEST(EventsIo, Errors) {
	TestEventLoop loop;

	int fds[2];
	ASSERT_EQ(pipe(fds), 0);

	events::io::AsyncFileDescriptorReader reader(loop, fds[0]);
	events::io::AsyncFileDescriptorWriter writer(loop, fds[1]);

	const uint8_t data[] = "abcd";

	vector<uint8_t> buf(data, data + sizeof(data));

	auto err = reader.AsyncRead(buf.end(), buf.begin(), [](io::ExpectedSize result) {});
	EXPECT_NE(err, error::NoError);
	EXPECT_EQ(err.code, make_error_condition(errc::invalid_argument));

	err = reader.AsyncRead(buf.begin(), buf.end(), nullptr);
	EXPECT_NE(err, error::NoError);
	EXPECT_EQ(err.code, make_error_condition(errc::invalid_argument));

	err = writer.AsyncWrite(buf.end(), buf.begin(), [](io::ExpectedSize result) {});
	EXPECT_NE(err, error::NoError);
	EXPECT_EQ(err.code, make_error_condition(errc::invalid_argument));

	err = writer.AsyncWrite(buf.begin(), buf.end(), nullptr);
	EXPECT_NE(err, error::NoError);
	EXPECT_EQ(err.code, make_error_condition(errc::invalid_argument));
}

TEST(EventsIo, CloseWriter) {
	TestEventLoop loop;

	int fds[2];
	ASSERT_EQ(pipe(fds), 0);

	events::io::AsyncFileDescriptorReader reader(loop, fds[0]);

	const uint8_t data[] = "abcd";

	vector<uint8_t> buf(data, data + sizeof(data));

	auto err = reader.AsyncRead(buf.begin(), buf.end(), [&loop](io::ExpectedSize result) {
		EXPECT_TRUE(result);
		EXPECT_EQ(result.value(), 0);

		loop.Stop();
	});
	ASSERT_EQ(err, error::NoError);

	close(fds[1]);
	loop.Run();
}

TEST(EventsIo, CloseReader) {
	TestEventLoop loop;

	int fds[2];
	ASSERT_EQ(pipe(fds), 0);

	events::io::AsyncFileDescriptorWriter writer(loop, fds[1]);
	close(fds[0]);

	const uint8_t data[] = "abcd";

	vector<uint8_t> buf(data, data + sizeof(data));

	auto err = writer.AsyncWrite(buf.begin(), buf.end(), [&loop](io::ExpectedSize result) {
		EXPECT_EQ(result.error().code, make_error_condition(errc::broken_pipe));

		loop.Stop();
	});
	ASSERT_EQ(err, error::NoError);

	loop.Run();
}

TEST(EventsIo, CancelWrite) {
	TestEventLoop loop;

	int fds[2];
	ASSERT_EQ(pipe(fds), 0);

	events::io::AsyncFileDescriptorReader reader(loop, fds[0]);
	events::io::AsyncFileDescriptorWriter writer(loop, fds[1]);

	const uint8_t data[] = "abcd";

	vector<uint8_t> to_send(data, data + sizeof(data));
	vector<uint8_t> to_receive;
	to_receive.resize(to_send.size());

	auto err =
		reader.AsyncRead(to_receive.begin(), to_receive.end(), [](io::ExpectedSize result) {});
	ASSERT_EQ(err, error::NoError);
	err = writer.AsyncWrite(to_send.begin(), to_send.end(), [](io::ExpectedSize result) {
		// Apparently AsyncWrite can immediately finish, so by the time we call Cancel(),
		// the operation is already done. So both responses are ok here.
		if (result) {
			EXPECT_EQ(result.value(), 5);
		} else {
			EXPECT_EQ(result.error().code, make_error_condition(errc::operation_canceled));
		}
	});
	ASSERT_EQ(err, error::NoError);

	mender::common::events::Timer timer {loop};
	timer.AsyncWait(chrono::milliseconds(100), [&loop](error::Error err) { loop.Stop(); });

	writer.Cancel();

	loop.Run();
}

TEST(EventsIo, CancelRead) {
	TestEventLoop loop;

	int fds[2];
	ASSERT_EQ(pipe(fds), 0);

	events::io::AsyncFileDescriptorReader reader(loop, fds[0]);
	events::io::AsyncFileDescriptorWriter writer(loop, fds[1]);

	const uint8_t data[] = "abcd";

	vector<uint8_t> to_send(data, data + sizeof(data));
	vector<uint8_t> to_receive;
	to_receive.resize(to_send.size());

	bool in_write {false};

	auto err = reader.AsyncRead(to_receive.begin(), to_receive.end(), [](io::ExpectedSize result) {
		ASSERT_FALSE(result);
		EXPECT_EQ(result.error().code, make_error_condition(errc::operation_canceled));
	});
	ASSERT_EQ(err, error::NoError);
	err = writer.AsyncWrite(
		to_send.begin(), to_send.end(), [&in_write](io::ExpectedSize result) { in_write = true; });
	ASSERT_EQ(err, error::NoError);

	mender::common::events::Timer timer {loop};
	timer.AsyncWait(chrono::milliseconds(100), [&loop](error::Error err) { loop.Stop(); });

	reader.Cancel();

	loop.Run();

	EXPECT_TRUE(in_write);
}

TEST(EventsIo, FileOpen) {
	mtesting::TemporaryDirectory tmpdir;
	TestEventLoop loop;
	string tmpfile = path::Join(tmpdir.Path(), "file");
	string stuff {"stuff"};
	vector<uint8_t> send(stuff.begin(), stuff.end());
	vector<uint8_t> recv;
	recv.resize(100);

	events::io::AsyncFileDescriptorWriter w(loop);
	auto err = w.Open(tmpfile);
	EXPECT_EQ(err, error::NoError);

	w.AsyncWrite(send.begin(), send.end(), [&loop](io::ExpectedSize result) {
		EXPECT_TRUE(result);
		EXPECT_EQ(result.value(), 5);

		loop.Stop();
	});

	loop.Run();

	// Should not destroy the content, due to Append.
	events::io::AsyncFileDescriptorWriter w2(loop);
	err = w2.Open(tmpfile, events::io::Append::Enabled);
	EXPECT_EQ(err, error::NoError);

	events::io::AsyncFileDescriptorReader r(loop);
	err = r.Open(tmpfile);
	EXPECT_EQ(err, error::NoError);

	r.AsyncRead(recv.begin(), recv.end(), [&loop](io::ExpectedSize result) {
		EXPECT_TRUE(result);
		EXPECT_EQ(result.value(), 5);

		loop.Stop();
	});

	loop.Run();

	EXPECT_EQ(string(recv.begin(), recv.begin() + 5), "stuff");
}

TEST(EventsIo, FileOpenErrors) {
	TestEventLoop loop;
	mtesting::TemporaryDirectory tmpdir;
	string tmpfile = tmpdir.Path() + "does/not/exist";

	events::io::AsyncFileDescriptorWriter w(loop);
	auto err = w.Open(tmpfile);
	EXPECT_NE(err, error::NoError);
	EXPECT_EQ(err.code, make_error_condition(errc::no_such_file_or_directory));

	events::io::AsyncFileDescriptorReader r(loop);
	err = r.Open(tmpfile);
	EXPECT_NE(err, error::NoError);
	EXPECT_EQ(err.code, make_error_condition(errc::no_such_file_or_directory));
}

TEST(EventsIo, DestroyWriterBeforeHandlerIsCalled) {
	TestEventLoop loop;

	int fds[2];
	ASSERT_EQ(pipe(fds), 0);

	events::io::AsyncFileDescriptorReader reader(loop, fds[0]);
	auto writer = make_shared<events::io::AsyncFileDescriptorWriter>(loop, fds[1]);

	const uint8_t data[] = "abcd";

	vector<uint8_t> to_send(data, data + sizeof(data));
	vector<uint8_t> to_receive;
	to_receive.resize(to_send.size());

	auto err =
		reader.AsyncRead(to_receive.begin(), to_receive.end(), [](io::ExpectedSize result) {});
	ASSERT_EQ(err, error::NoError);
	err = writer->AsyncWrite(to_send.begin(), to_send.end(), [](io::ExpectedSize result) {
		FAIL() << "Should never get here ";
	});
	ASSERT_EQ(err, error::NoError);

	mender::common::events::Timer timer {loop};
	timer.AsyncWait(chrono::milliseconds(100), [&loop](error::Error err) { loop.Stop(); });

	writer.reset();

	loop.Run();
}

TEST(EventsIo, DestroyReaderBeforeHandlerIsCalled) {
	TestEventLoop loop;

	int fds[2];
	ASSERT_EQ(pipe(fds), 0);

	auto reader = make_shared<events::io::AsyncFileDescriptorReader>(loop, fds[0]);
	events::io::AsyncFileDescriptorWriter writer(loop, fds[1]);

	const uint8_t data[] = "abcd";

	vector<uint8_t> to_send(data, data + sizeof(data));
	vector<uint8_t> to_receive;
	to_receive.resize(to_send.size());

	bool in_write {false};

	auto err = reader->AsyncRead(to_receive.begin(), to_receive.end(), [](io::ExpectedSize result) {
		FAIL() << "Should never get here ";
	});
	ASSERT_EQ(err, error::NoError);
	err = writer.AsyncWrite(
		to_send.begin(), to_send.end(), [&in_write, &reader](io::ExpectedSize result) {
			in_write = true;
			reader.reset();
		});
	ASSERT_EQ(err, error::NoError);

	mender::common::events::Timer timer {loop};
	timer.AsyncWait(chrono::milliseconds(100), [&loop](error::Error err) { loop.Stop(); });

	loop.Run();

	EXPECT_TRUE(in_write);
}

TEST(EventsIo, AsyncIoFromSyncIo) {
	TestEventLoop loop;

	string input {"abcd"};

	io::ReaderPtr reader = make_shared<io::StringReader>(input);

	vector<uint8_t> output;
	output.resize(100);

	io::WriterPtr writer = make_shared<io::ByteWriter>(output);

	auto areader = make_shared<events::io::AsyncReaderFromReader>(loop, reader);
	auto awriter = make_shared<events::io::AsyncWriterFromWriter>(loop, writer);

	vector<uint8_t> tmp;
	tmp.resize(100);

	auto err = areader->AsyncRead(
		tmp.begin(), tmp.end(), [&tmp, &input, awriter, &loop](io::ExpectedSize result) {
			EXPECT_EQ(result.value(), input.size());
			ASSERT_TRUE(result);

			auto err = awriter->AsyncWrite(
				tmp.begin(),
				tmp.begin() + result.value(),
				[&input, &loop](io::ExpectedSize result2) {
					EXPECT_EQ(result2.value(), input.size());
					ASSERT_TRUE(result2);

					loop.Stop();
				});
			ASSERT_EQ(err, error::NoError);
		});
	ASSERT_EQ(err, error::NoError);

	loop.Run();

	EXPECT_EQ(string(output.begin(), output.begin() + input.size()), input);
}

// Dummy reader that detects the number of '1' in a stream. It is meant to verify that it
// actually reads the stream together with the main reader, and can fail the EOF Read if necessary
class CountOnesReader : virtual public io::AsyncReader {
private:
	io::AsyncReader &wrapped_reader_;
	int found_ones_ {0};
	int expected_ones_ {0};

public:
	CountOnesReader(io::AsyncReader &reader, const int ones) :
		wrapped_reader_ {reader},
		expected_ones_(ones) {};

	error::Error AsyncRead(
		vector<uint8_t>::iterator start,
		vector<uint8_t>::iterator end,
		io::AsyncIoHandler handler) override {
		return wrapped_reader_.AsyncRead(
			start, end, [this, start, handler](io::ExpectedSize result) {
				if (!result) {
					handler(result);
				}

				for (auto &it : vector<uint8_t> {start, start + result.value()}) {
					if (it == '1') {
						found_ones_++;
					}
				}

				if ((result.value() == 0) && (found_ones_ != expected_ones_)) {
					handler(expected::unexpected(
						error::MakeError(error::GenericError, "ones mismatch")));
					return;
				}

				handler(result);
			});
	};

	void Cancel() override {
		wrapped_reader_.Cancel();
	};
};

TEST(EventsIo, TeeReaderSimpleCase) {
	TestEventLoop loop;

	int fds[2];
	ASSERT_EQ(pipe(fds), 0);

	events::io::AsyncFileDescriptorReader reader(loop, fds[0]);
	events::io::AsyncFileDescriptorWriter writer(loop, fds[1]);

	const uint8_t data[] = "abcd1efgh1";

	vector<uint8_t> to_send(data, data + sizeof(data));

	vector<uint8_t> buffer1;
	buffer1.resize(to_send.size());

	vector<uint8_t> buffer2;
	buffer2.resize(to_send.size());

	events::io::TeeReaderPtr upstream_reader {make_shared<events::io::TeeReader>(reader)};

	auto downstream_reader1 = upstream_reader->MakeAsyncReader();
	ASSERT_TRUE(downstream_reader1) << downstream_reader1.error().String();
	auto downstream_reader2 = upstream_reader->MakeAsyncReader();
	ASSERT_TRUE(downstream_reader2) << downstream_reader2.error().String();

	bool eof_reader1 {false};
	bool eof_reader2 {false};

	// Two leaf readers, the second one shall fail after EOF
	auto one_reader1 = CountOnesReader(*downstream_reader1.value(), 2);
	auto one_reader2 = CountOnesReader(*downstream_reader2.value(), 22);

	auto err = one_reader1.AsyncRead(
		buffer1.begin(),
		buffer1.end(),
		[&one_reader1, &buffer1, &eof_reader1](io::ExpectedSize result) {
			ASSERT_TRUE(result) << result.error().String();
			EXPECT_EQ(result.value(), 11);

			auto err = one_reader1.AsyncRead(
				buffer1.begin(), buffer1.end(), [&eof_reader1](io::ExpectedSize result) {
					eof_reader1 = true;
					ASSERT_TRUE(result) << result.error().String();
					EXPECT_EQ(result.value(), 0);
				});
			ASSERT_EQ(err, error::NoError);
		});
	ASSERT_EQ(err, error::NoError);

	err = one_reader2.AsyncRead(
		buffer2.begin(),
		buffer2.end(),
		[&one_reader2, &buffer2, &eof_reader2](io::ExpectedSize result) {
			ASSERT_TRUE(result) << result.error().String();
			EXPECT_EQ(result.value(), 11);

			auto err = one_reader2.AsyncRead(
				buffer2.begin(), buffer2.end(), [&eof_reader2](io::ExpectedSize result) {
					eof_reader2 = true;
					ASSERT_FALSE(result) << result.value();
					EXPECT_EQ(result.error().message, "ones mismatch");
				});
			ASSERT_EQ(err, error::NoError);
		});
	ASSERT_EQ(err, error::NoError);

	err = writer.AsyncWrite(to_send.begin(), to_send.end(), [&fds](io::ExpectedSize result) {
		EXPECT_TRUE(result);
		EXPECT_EQ(result.value(), 11);
		close(fds[1]);
	});
	ASSERT_EQ(err, error::NoError);

	events::Timer timer(loop);
	timer.AsyncWait(chrono::milliseconds(1), [&loop](error::Error err) {
		ASSERT_EQ(err, error::NoError);
		loop.Stop();
	});

	loop.Run();

	EXPECT_EQ(buffer1, to_send);
	EXPECT_EQ(buffer2, to_send);
	EXPECT_TRUE(eof_reader1);
	EXPECT_TRUE(eof_reader2);
}

TEST(EventsIo, TeeReaderShortReads) {
	TestEventLoop loop;

	int fds[2];
	ASSERT_EQ(pipe(fds), 0);

	events::io::AsyncFileDescriptorReader reader(loop, fds[0]);
	events::io::AsyncFileDescriptorWriter writer(loop, fds[1]);

	const uint8_t data[] = "abcd1efgh1";

	vector<uint8_t> to_send(data, data + sizeof(data));

	vector<uint8_t> buffer1;
	buffer1.resize(to_send.size());

	vector<uint8_t> buffer2;
	buffer2.resize(to_send.size());

	events::io::TeeReaderPtr upstream_reader {make_shared<events::io::TeeReader>(reader)};

	auto downstream_reader1 = upstream_reader->MakeAsyncReader();
	ASSERT_TRUE(downstream_reader1) << downstream_reader1.error().String();
	auto downstream_reader2 = upstream_reader->MakeAsyncReader();
	ASSERT_TRUE(downstream_reader2) << downstream_reader2.error().String();

	bool eof_reader1 {false};
	bool eof_reader2 {false};

	// Two leaf readers, the second one shall fail after EOF
	auto one_reader1 = CountOnesReader(*downstream_reader1.value(), 2);
	auto one_reader2 = CountOnesReader(*downstream_reader2.value(), 22);

	// First call, short read
	auto err = one_reader1.AsyncRead(
		buffer1.begin(),
		buffer1.begin() + 5,
		[&one_reader1, &buffer1, &eof_reader1](io::ExpectedSize result) {
			ASSERT_TRUE(result) << result.error().String();
			EXPECT_EQ(result.value(), 5);

			// Second call, remaining data
			auto err = one_reader1.AsyncRead(
				buffer1.begin() + 5,
				buffer1.end(),
				[&one_reader1, &buffer1, &eof_reader1](io::ExpectedSize result) {
					ASSERT_TRUE(result) << result.error().String();
					EXPECT_EQ(result.value(), 6);

					// Last call, EOF
					auto err = one_reader1.AsyncRead(
						buffer1.begin(), buffer1.end(), [&eof_reader1](io::ExpectedSize result) {
							eof_reader1 = true;
							ASSERT_TRUE(result) << result.error().String();
							EXPECT_EQ(result.value(), 0);
						});
					ASSERT_EQ(err, error::NoError);
				});
			ASSERT_EQ(err, error::NoError);
		});
	ASSERT_EQ(err, error::NoError);

	// First call, short read (forced by reader1)
	err = one_reader2.AsyncRead(
		buffer2.begin(),
		buffer2.end(),
		[&one_reader2, &buffer2, &eof_reader2](io::ExpectedSize result) {
			ASSERT_TRUE(result) << result.error().String();
			EXPECT_EQ(result.value(), 5);

			// Second call, remaining data
			auto err = one_reader2.AsyncRead(
				buffer2.begin() + 5,
				buffer2.end(),
				[&one_reader2, &buffer2, &eof_reader2](io::ExpectedSize result) {
					ASSERT_TRUE(result) << result.error().String();
					EXPECT_EQ(result.value(), 6);

					// Last call, EOF
					auto err = one_reader2.AsyncRead(
						buffer2.begin(), buffer2.end(), [&eof_reader2](io::ExpectedSize result) {
							eof_reader2 = true;
							ASSERT_FALSE(result) << result.value();
							EXPECT_EQ(result.error().message, "ones mismatch");
						});
					ASSERT_EQ(err, error::NoError);
				});
			ASSERT_EQ(err, error::NoError);
		});
	ASSERT_EQ(err, error::NoError);

	// Single write
	err = writer.AsyncWrite(to_send.begin(), to_send.end(), [&fds](io::ExpectedSize result) {
		EXPECT_TRUE(result);
		EXPECT_EQ(result.value(), 11);
		close(fds[1]);
	});
	ASSERT_EQ(err, error::NoError);

	events::Timer timer(loop);
	timer.AsyncWait(chrono::milliseconds(1), [&loop](error::Error err) {
		ASSERT_EQ(err, error::NoError);
		loop.Stop();
	});

	loop.Run();

	EXPECT_EQ(buffer1, to_send);
	EXPECT_EQ(buffer2, to_send);
	EXPECT_TRUE(eof_reader1);
	EXPECT_TRUE(eof_reader2);
}

TEST(EventsIo, TeeReaderBufferedContents) {
	TestEventLoop loop;

	int fds[2];
	ASSERT_EQ(pipe(fds), 0);

	events::io::AsyncFileDescriptorReader reader(loop, fds[0]);
	events::io::AsyncFileDescriptorWriter writer(loop, fds[1]);

	const uint8_t data[] = "abcd1efgh1";

	vector<uint8_t> to_send(data, data + sizeof(data));

	vector<uint8_t> buffer1;
	buffer1.resize(to_send.size());

	vector<uint8_t> buffer2;
	buffer2.resize(to_send.size());

	events::io::TeeReaderPtr upstream_reader {make_shared<events::io::TeeReader>(reader)};

	auto downstream_reader1 = upstream_reader->MakeAsyncReader();
	ASSERT_TRUE(downstream_reader1) << downstream_reader1.error().String();

	bool eof_reader1 {false};
	bool eof_reader2 {false};

	// Two leaf readers, the second one should succeed in getting all data
	auto raw_reader1 = downstream_reader1.value();
	io::AsyncReaderPtr one_reader2;

	// First call, short read
	auto err = raw_reader1->AsyncRead(
		buffer1.begin(),
		buffer1.begin() + 5,
		[&upstream_reader,
		 &raw_reader1,
		 &one_reader2,
		 &buffer1,
		 &buffer2,
		 &eof_reader1,
		 &eof_reader2](io::ExpectedSize result) {
			ASSERT_TRUE(result) << result.error().String();
			EXPECT_EQ(result.value(), 5);

			// Attach new reader
			auto downstream_reader2 = upstream_reader->MakeAsyncReader();
			ASSERT_TRUE(downstream_reader2) << downstream_reader2.error().String();
			one_reader2 = make_shared<CountOnesReader>(*downstream_reader2.value(), 2);

			// Second call, remaining data
			auto err = raw_reader1->AsyncRead(
				buffer1.begin() + 5,
				buffer1.end(),
				[&raw_reader1, &buffer1, &eof_reader1](io::ExpectedSize result) {
					ASSERT_TRUE(result) << result.error().String();
					EXPECT_EQ(result.value(), 6);

					// Third call, EOF
					auto err = raw_reader1->AsyncRead(
						buffer1.begin(), buffer1.end(), [&eof_reader1](io::ExpectedSize result) {
							eof_reader1 = true;
							ASSERT_TRUE(result) << result.error().String();
							EXPECT_EQ(result.value(), 0);
						});
					ASSERT_EQ(err, error::NoError);
				});
			ASSERT_EQ(err, error::NoError);

			// First call for reader2, it shall get the buffered data
			err = one_reader2->AsyncRead(
				buffer2.begin(),
				buffer2.end(),
				[&one_reader2, &buffer2, &eof_reader2](io::ExpectedSize result) {
					ASSERT_TRUE(result) << result.error().String();
					EXPECT_EQ(result.value(), 5);

					// Second call, remaining data
					auto err = one_reader2->AsyncRead(
						buffer2.begin() + 5,
						buffer2.end(),
						[&one_reader2, &buffer2, &eof_reader2](io::ExpectedSize result) {
							ASSERT_TRUE(result) << result.error().String();
							EXPECT_EQ(result.value(), 6);

							// Third call, EOF
							auto err = one_reader2->AsyncRead(
								buffer2.begin(),
								buffer2.end(),
								[&eof_reader2](io::ExpectedSize result) {
									eof_reader2 = true;
									ASSERT_TRUE(result) << result.error().String();
									EXPECT_EQ(result.value(), 0);
								});
							ASSERT_EQ(err, error::NoError);
						});
					ASSERT_EQ(err, error::NoError);
				});
			ASSERT_EQ(err, error::NoError);
		});
	ASSERT_EQ(err, error::NoError);

	// Single write
	err = writer.AsyncWrite(to_send.begin(), to_send.end(), [&fds](io::ExpectedSize result) {
		EXPECT_TRUE(result);
		EXPECT_EQ(result.value(), 11);
		close(fds[1]);
	});
	ASSERT_EQ(err, error::NoError);

	events::Timer timer(loop);
	timer.AsyncWait(chrono::milliseconds(1), [&loop](error::Error err) {
		ASSERT_EQ(err, error::NoError);
		loop.Stop();
	});

	loop.Run();

	EXPECT_EQ(buffer1, to_send);
	EXPECT_EQ(buffer2, to_send);
	EXPECT_TRUE(eof_reader1);
	EXPECT_TRUE(eof_reader2);
}
