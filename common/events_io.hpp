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

#ifndef MENDER_COMMON_IO_UTIL_HPP
#define MENDER_COMMON_IO_UTIL_HPP

#include <memory>
#include <vector>

#include <boost/asio.hpp>

#include <common/events.hpp>
#include <common/io.hpp>

namespace mender {
namespace common {
namespace events {
namespace io {

using namespace std;

namespace asio = boost::asio;

enum class Append {
	Disabled,
	Enabled,
};

class AsyncFileDescriptorReader :
	public EventLoopObject,
	virtual public mender::common::io::AsyncReader {
public:
	// Takes ownership of fd.
	explicit AsyncFileDescriptorReader(events::EventLoop &loop, int fd);
	explicit AsyncFileDescriptorReader(events::EventLoop &loop);
	~AsyncFileDescriptorReader();

	error::Error Open(const string &path);

	error::Error AsyncRead(
		vector<uint8_t>::iterator start,
		vector<uint8_t>::iterator end,
		mender::common::io::AsyncIoHandler handler) override;
	void Cancel() override;

private:
#ifdef MENDER_USE_BOOST_ASIO
	asio::posix::stream_descriptor pipe_;
	shared_ptr<bool> cancelled_;
#endif // MENDER_USE_BOOST_ASIO
};

class AsyncFileDescriptorWriter :
	public EventLoopObject,
	virtual public mender::common::io::AsyncWriter {
public:
	// Takes ownership of fd.
	explicit AsyncFileDescriptorWriter(events::EventLoop &loop, int fd);
	explicit AsyncFileDescriptorWriter(events::EventLoop &loop);
	~AsyncFileDescriptorWriter();

	error::Error Open(const string &path, Append append = Append::Disabled);

	error::Error AsyncWrite(
		vector<uint8_t>::const_iterator start,
		vector<uint8_t>::const_iterator end,
		mender::common::io::AsyncIoHandler handler) override;
	void Cancel() override;

private:
#ifdef MENDER_USE_BOOST_ASIO
	asio::posix::stream_descriptor pipe_;
	shared_ptr<bool> cancelled_;
#endif // MENDER_USE_BOOST_ASIO
};

} // namespace io
} // namespace events
} // namespace common
} // namespace mender

#endif // MENDER_COMMON_IO_UTIL_HPP
