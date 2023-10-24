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

#include <common/http.hpp>

#include <cstdlib>
#include <memory>

#include <gmock/gmock.h>
#include <gtest/gtest.h>

#include <common/common.hpp>
#include <common/error.hpp>
#include <common/http.hpp>
#include <common/io.hpp>
#include <common/path.hpp>
#include <common/processes.hpp>
#include <common/testing.hpp>

using namespace std;

#define TEST_PORT "8001"
#define TEST_TLS_PORT "8002"
#define TEST_PROXY_PORT "8003"
#define TEST_TLS_PROXY_PORT "8004"
#define TEST_CLOSED_PORT "8005"

namespace common = mender::common;
namespace error = mender::common::error;
namespace http = mender::http;
namespace io = mender::common::io;
namespace path = mender::common::path;
namespace processes = mender::common::processes;
namespace mtesting = mender::common::testing;

class HttpProxyTest : public testing::Test {
public:
	void EnsureUp(processes::Process &proc) {
		chrono::steady_clock clock;
		auto started = clock.now();
		proc.Run();
		while (proc.GetExitStatus() != 0) {
			ASSERT_LT(clock.now() - started, chrono::seconds(1)) << "Timed out waiting for service";

			std::this_thread::sleep_for(std::chrono::milliseconds {10});
			proc.Run();
		}
	}

	void StartPlainServer() {
		http::ServerConfig server_config;
		plain_server.reset(new http::Server(server_config, loop));
		auto err = plain_server->AsyncServeUrl(
			"http://127.0.0.1:" TEST_PORT,
			[this](http::ExpectedIncomingRequestPtr exp_req) {
				if (!exp_req && exp_req.error().String().find("end of stream") != string::npos) {
					// This happens while we are bringing the TLS servers up.
					return;
				}
				ASSERT_TRUE(exp_req) << exp_req.error().String();

				plain_server_hit_header = true;

				EXPECT_EQ(exp_req.value()->GetPath(), "/index.html");
			},
			[this](http::ExpectedIncomingRequestPtr exp_req) {
				plain_server_hit_body = true;
				ASSERT_TRUE(exp_req) << exp_req.error().String();

				auto result = exp_req.value()->MakeResponse();
				ASSERT_TRUE(result);
				auto resp = result.value();

				string body = "Test\r\n";
				auto body_writer = make_shared<io::StringReader>(body);
				resp->SetBodyReader(body_writer);
				resp->SetHeader("Content-Length", to_string(body.size()));

				resp->SetStatusCodeAndMessage(200, "Success");
				resp->AsyncReply([](error::Error err) { ASSERT_EQ(error::NoError, err); });
			});
		ASSERT_EQ(error::NoError, err);
	}

	void StartProxy() {
		const string tiny_proxy = "/usr/bin/tinyproxy";
		const string nc = "/bin/nc";

		// Skip these tests if tinyproxy or nc are not available, since they are not
		// standard tools. However, if we are running in the CI, we never skip.
		if ((!path::FileExists(tiny_proxy) || !path::FileExists(nc))
			&& (getenv("CI") == nullptr || string(getenv("CI")) == "")) {
			GTEST_SKIP() << "tinyproxy not available";
		}

		string config_file = path::Join(tmpdir.Path(), "tinyproxy.conf");
		ofstream config(config_file);
		config << R"(Port )" << TEST_PROXY_PORT << R"(
Listen 127.0.0.1
Timeout 10
Allow 127.0.0.1
MaxClients 10
StartServers 1
)";
		ASSERT_TRUE(config.good());
		config.close();

		proxy.reset(new processes::Process({tiny_proxy, "-d", "-c", config_file}));
		auto err = proxy->Start();
		ASSERT_EQ(err, error::NoError) << err.String();

		// Check when the proxy is up.
		processes::Process nc_proc {{"nc", "-z", "127.0.0.1", TEST_PROXY_PORT}};
		EnsureUp(nc_proc);
	}

	void StartTlsTunnel(const string &listen_port, const string &connect_port) {
		const string stunnel = "/usr/bin/stunnel4";

		// Skip these tests if stunnel4 is not available, since it is not a standard
		// tool. However, if we are running in the CI, we never skip.
		if (!path::FileExists(stunnel) && (getenv("CI") == nullptr || string(getenv("CI")) == "")) {
			GTEST_SKIP() << "stunnel4 not available";
		}

		string config_file = path::Join(tmpdir.Path(), "stunnel.conf");

		{
			ofstream conf(config_file);
			conf << R"(foreground = yes
pid =

[tls_proxy_gateway]
cert = server.localhost.crt
key = server.localhost.key
retry = yes
accept = )" + listen_port
						+ R"(
connect = localhost:)" + connect_port
						+ R"(
)";
			ASSERT_TRUE(conf.good());
		}

		proxy_tls_gateway.reset(new processes::Process({stunnel, config_file}));
		auto err = proxy_tls_gateway->Start();
		ASSERT_EQ(err, error::NoError) << err.String();

		// Check when the server is up.
		processes::Process client {
			{"bash",
			 "-c",
			 R"(openssl s_client -CAfile server.localhost.crt -connect localhost:)" + listen_port
				 + R"( < /dev/null |grep "Verification: OK")"}};
		EnsureUp(client);
	}

	void StartTlsServer() {
		tls_server.reset(new processes::Process(
			{"openssl",
			 "s_server",
			 "-HTTP",
			 "-key",
			 "server.localhost.key",
			 "-cert",
			 "server.localhost.crt",
			 "-accept",
			 TEST_TLS_PORT}));
		auto err = tls_server->Start();
		ASSERT_EQ(err, error::NoError);

		// Check when the server is up.
		processes::Process client {
			{"bash",
			 "-c",
			 R"(openssl s_client -CAfile server.localhost.crt -connect localhost:)" TEST_TLS_PORT
			 R"( < /dev/null |grep "Verification: OK")"}};
		EnsureUp(client);
	}

	void StartTlsProxy() {
		StartProxy();
		StartTlsTunnel(TEST_TLS_PROXY_PORT, TEST_PROXY_PORT);
	}

	mtesting::TemporaryDirectory tmpdir;

	mtesting::TestEventLoop loop;

	unique_ptr<http::Server> plain_server;
	unique_ptr<processes::Process> proxy;
	unique_ptr<processes::Process> proxy_tls_gateway;
	unique_ptr<processes::Process> tls_server;

	bool plain_server_hit_header;
	bool plain_server_hit_body;
};

TEST_F(HttpProxyTest, HostNameMatchesNoProxy) {
	using http::HostNameMatchesNoProxy;

	EXPECT_FALSE(HostNameMatchesNoProxy("127.0.0.1", ""));
	EXPECT_TRUE(HostNameMatchesNoProxy("127.0.0.1", "127.0.0.1"));

	EXPECT_TRUE(HostNameMatchesNoProxy("northern.tech", "northern.tech"));
	EXPECT_TRUE(HostNameMatchesNoProxy("northern.tech", "other.tech northern.tech"));
	EXPECT_TRUE(HostNameMatchesNoProxy("northern.tech", "northern.tech other.tech"));
	EXPECT_TRUE(HostNameMatchesNoProxy("northern.tech", "other.tech northern.tech other.tech"));

	EXPECT_FALSE(HostNameMatchesNoProxy("sub.northern.tech", "northern.tech"));
	EXPECT_FALSE(HostNameMatchesNoProxy("sub.northern.tech", "other.tech northern.tech"));
	EXPECT_FALSE(HostNameMatchesNoProxy("sub.northern.tech", "northern.tech other.tech"));
	EXPECT_FALSE(
		HostNameMatchesNoProxy("sub.northern.tech", "other.tech northern.tech other.tech"));

	EXPECT_TRUE(HostNameMatchesNoProxy("sub.northern.tech", ".northern.tech"));
	EXPECT_TRUE(HostNameMatchesNoProxy("sub.northern.tech", ".other.tech .northern.tech"));
	EXPECT_TRUE(HostNameMatchesNoProxy("sub.northern.tech", ".northern.tech .other.tech"));
	EXPECT_TRUE(
		HostNameMatchesNoProxy("sub.northern.tech", ".other.tech .northern.tech .other.tech"));

	// Degenerate case, mostly to test that it doesn't crash.
	EXPECT_TRUE(HostNameMatchesNoProxy("sub.northern.tech", "."));
}

// HTTP proxy with HTTP requests.
class HttpProxyHttpTest : public HttpProxyTest {
public:
	void SetUp() override {
		StartPlainServer();
		StartProxy();
	}
};

TEST_F(HttpProxyHttpTest, BasicRequestAndResponse) {
	bool client_hit_header = false;
	bool client_hit_body = false;

	http::ClientConfig client_config {
		.http_proxy = "http://127.0.0.1:" TEST_PROXY_PORT,
	};
	http::Client client(client_config, loop);
	auto req = make_shared<http::OutgoingRequest>();
	req->SetMethod(http::Method::GET);
	req->SetAddress("http://127.0.0.1:" TEST_PORT "/index.html");
	vector<uint8_t> received;
	auto err = client.AsyncCall(
		req,
		[&client_hit_header, &received](http::ExpectedIncomingResponsePtr exp_resp) {
			ASSERT_TRUE(exp_resp) << exp_resp.error().String();
			auto resp = exp_resp.value();
			EXPECT_EQ(resp->GetStatusCode(), 200);
			client_hit_header = true;

			auto body_writer = make_shared<io::ByteWriter>(received);
			body_writer->SetUnlimited(true);
			resp->SetBodyWriter(body_writer);
		},
		[&client_hit_body, this](http::ExpectedIncomingResponsePtr exp_resp) {
			ASSERT_TRUE(exp_resp) << exp_resp.error().String();
			client_hit_body = true;
			loop.Stop();
		});
	ASSERT_EQ(error::NoError, err);

	loop.Run();

	EXPECT_TRUE(plain_server_hit_header);
	EXPECT_TRUE(plain_server_hit_body);
	EXPECT_TRUE(client_hit_header);
	EXPECT_TRUE(client_hit_body);
	EXPECT_EQ(common::StringFromByteVector(received), "Test\r\n");
}

TEST_F(HttpProxyHttpTest, TargetInNoProxy) {
	bool client_hit_header = false;
	bool client_hit_body = false;

	http::ClientConfig client_config {
		.http_proxy = "http://127.0.0.1:1",
		.no_proxy = "127.0.0.1",
	};
	http::Client client(client_config, loop);
	auto req = make_shared<http::OutgoingRequest>();
	req->SetMethod(http::Method::GET);
	req->SetAddress("http://127.0.0.1:" TEST_PORT "/index.html");
	vector<uint8_t> received;
	auto err = client.AsyncCall(
		req,
		[&client_hit_header, &received](http::ExpectedIncomingResponsePtr exp_resp) {
			ASSERT_TRUE(exp_resp) << exp_resp.error().String();
			auto resp = exp_resp.value();
			EXPECT_EQ(resp->GetStatusCode(), 200);
			client_hit_header = true;

			auto body_writer = make_shared<io::ByteWriter>(received);
			body_writer->SetUnlimited(true);
			resp->SetBodyWriter(body_writer);
		},
		[&client_hit_body, this](http::ExpectedIncomingResponsePtr exp_resp) {
			ASSERT_TRUE(exp_resp) << exp_resp.error().String();
			client_hit_body = true;
			loop.Stop();
		});
	ASSERT_EQ(error::NoError, err);

	loop.Run();

	EXPECT_TRUE(plain_server_hit_header);
	EXPECT_TRUE(plain_server_hit_body);
	EXPECT_TRUE(client_hit_header);
	EXPECT_TRUE(client_hit_body);
	EXPECT_EQ(common::StringFromByteVector(received), "Test\r\n");
}

TEST_F(HttpProxyHttpTest, WrongProxySet) {
	bool client_hit_header = false;

	http::ClientConfig client_config {
		.http_proxy = "http://127.0.0.1:1",
	};
	http::Client client(client_config, loop);
	auto req = make_shared<http::OutgoingRequest>();
	req->SetMethod(http::Method::GET);
	req->SetAddress("http://127.0.0.1:" TEST_PORT "/index.html");
	auto err = client.AsyncCall(
		req,
		[&client_hit_header, this](http::ExpectedIncomingResponsePtr exp_resp) {
			EXPECT_FALSE(exp_resp);
			client_hit_header = true;
			loop.Stop();
		},
		[](http::ExpectedIncomingResponsePtr exp_resp) {
			EXPECT_TRUE(false) << "Should not get here";
		});
	ASSERT_EQ(error::NoError, err);

	loop.Run();

	EXPECT_FALSE(plain_server_hit_header);
	EXPECT_FALSE(plain_server_hit_body);
	EXPECT_TRUE(client_hit_header);
}

TEST_F(HttpProxyHttpTest, BogusProxySet) {
	auto test = [this](const string &protocol, const http::ClientConfig &client_config) {
		http::Client client(client_config, loop);
		auto req = make_shared<http::OutgoingRequest>();
		req->SetMethod(http::Method::GET);
		req->SetAddress(protocol + "://localhost/index.html");

		auto handler = [](http::ExpectedIncomingResponsePtr exp_resp) {
			EXPECT_TRUE(false) << "Should not get here";
		};
		return client.AsyncCall(req, handler, handler);
	};

	auto err = test("http", http::ClientConfig {.http_proxy = "bogus"});
	EXPECT_NE(error::NoError, err);
	EXPECT_EQ(err.code, http::MakeError(http::InvalidUrlError, "").code) << err.String();

	err = test("http", http::ClientConfig {.http_proxy = "http://localhost/a-path"});
	EXPECT_NE(error::NoError, err);
	EXPECT_EQ(err.code, http::MakeError(http::InvalidUrlError, "").code) << err.String();

	err = test("https", http::ClientConfig {.https_proxy = "bogus"});
	EXPECT_NE(error::NoError, err);
	EXPECT_EQ(err.code, http::MakeError(http::InvalidUrlError, "").code) << err.String();

	err = test("https", http::ClientConfig {.https_proxy = "http://localhost/a-path"});
	EXPECT_NE(error::NoError, err);
	EXPECT_EQ(err.code, http::MakeError(http::InvalidUrlError, "").code) << err.String();
}

TEST_F(HttpProxyHttpTest, WrongTarget) {
	bool client_hit_header = false;
	bool client_hit_body = false;

	http::ClientConfig client_config {
		.http_proxy = "http://127.0.0.1:" TEST_PROXY_PORT,
	};
	http::Client client(client_config, loop);
	auto req = make_shared<http::OutgoingRequest>();
	req->SetMethod(http::Method::GET);
	req->SetAddress("http://127.0.0.1:" TEST_CLOSED_PORT "/index.html");
	auto err = client.AsyncCall(
		req,
		[&client_hit_header](http::ExpectedIncomingResponsePtr exp_resp) {
			ASSERT_TRUE(exp_resp) << exp_resp.error().String();
			auto resp = exp_resp.value();
			EXPECT_EQ(resp->GetStatusCode(), 500);
			EXPECT_EQ(resp->GetStatusMessage(), "Unable to connect");
			client_hit_header = true;
		},
		[&client_hit_body, this](http::ExpectedIncomingResponsePtr exp_resp) {
			ASSERT_TRUE(exp_resp) << exp_resp.error().String();
			client_hit_body = true;
			loop.Stop();
		});
	ASSERT_EQ(error::NoError, err);

	loop.Run();

	EXPECT_TRUE(client_hit_header);
	EXPECT_TRUE(client_hit_body);
}

// HTTP proxy with HTTPS requests.
class HttpProxyHttpsTest : public HttpProxyTest {
public:
	void SetUp() override {
		StartProxy();
		StartTlsServer();
	}
};

TEST_F(HttpProxyHttpsTest, BasicRequestAndResponse) {
	bool client_hit_header = false;
	bool client_hit_body = false;

	http::ClientConfig client_config {
		.server_cert_path = "server.localhost.crt",
		.https_proxy = "http://localhost:" TEST_PROXY_PORT,
	};
	http::Client client(client_config, loop);
	auto req = make_shared<http::OutgoingRequest>();
	req->SetMethod(http::Method::GET);
	req->SetAddress("https://localhost:" TEST_TLS_PORT "/index.html");
	vector<uint8_t> received;
	auto err = client.AsyncCall(
		req,
		[&client_hit_header, &received](http::ExpectedIncomingResponsePtr exp_resp) {
			ASSERT_TRUE(exp_resp) << exp_resp.error().String();
			auto resp = exp_resp.value();
			EXPECT_EQ(resp->GetStatusCode(), 200);
			client_hit_header = true;

			auto body_writer = make_shared<io::ByteWriter>(received);
			body_writer->SetUnlimited(true);
			resp->SetBodyWriter(body_writer);
		},
		[&client_hit_body, this](http::ExpectedIncomingResponsePtr exp_resp) {
			ASSERT_TRUE(exp_resp) << exp_resp.error().String();
			client_hit_body = true;
			loop.Stop();
		});
	ASSERT_EQ(error::NoError, err);

	loop.Run();

	EXPECT_TRUE(client_hit_header);
	EXPECT_TRUE(client_hit_body);
	EXPECT_EQ(common::StringFromByteVector(received), "Test\r\n");
}

TEST_F(HttpProxyHttpsTest, TargetInNoProxy) {
	bool client_hit_header = false;
	bool client_hit_body = false;

	http::ClientConfig client_config {
		.server_cert_path = "server.localhost.crt",
		.https_proxy = "http://localhost:1",
		.no_proxy = "localhost",
	};
	http::Client client(client_config, loop);
	auto req = make_shared<http::OutgoingRequest>();
	req->SetMethod(http::Method::GET);
	req->SetAddress("https://localhost:" TEST_TLS_PORT "/index.html");
	vector<uint8_t> received;
	auto err = client.AsyncCall(
		req,
		[&client_hit_header, &received](http::ExpectedIncomingResponsePtr exp_resp) {
			ASSERT_TRUE(exp_resp) << exp_resp.error().String();
			auto resp = exp_resp.value();
			EXPECT_EQ(resp->GetStatusCode(), 200);
			client_hit_header = true;

			auto body_writer = make_shared<io::ByteWriter>(received);
			body_writer->SetUnlimited(true);
			resp->SetBodyWriter(body_writer);
		},
		[&client_hit_body, this](http::ExpectedIncomingResponsePtr exp_resp) {
			ASSERT_TRUE(exp_resp) << exp_resp.error().String();
			client_hit_body = true;
			loop.Stop();
		});
	ASSERT_EQ(error::NoError, err);

	loop.Run();

	EXPECT_TRUE(client_hit_header);
	EXPECT_TRUE(client_hit_body);
	EXPECT_EQ(common::StringFromByteVector(received), "Test\r\n");
}

TEST_F(HttpProxyHttpsTest, WrongProxySet) {
	bool client_hit_header = false;

	http::ClientConfig client_config {
		.server_cert_path = "server.localhost.crt",
		.https_proxy = "http://localhost:1",
	};
	http::Client client(client_config, loop);
	auto req = make_shared<http::OutgoingRequest>();
	req->SetMethod(http::Method::GET);
	req->SetAddress("https://localhost:" TEST_TLS_PORT "/index.html");
	auto err = client.AsyncCall(
		req,
		[&client_hit_header, this](http::ExpectedIncomingResponsePtr exp_resp) {
			EXPECT_FALSE(exp_resp);
			client_hit_header = true;
			loop.Stop();
		},
		[](http::ExpectedIncomingResponsePtr exp_resp) {
			EXPECT_TRUE(false) << "Should not get here";
		});
	ASSERT_EQ(error::NoError, err);

	loop.Run();

	EXPECT_TRUE(client_hit_header);
}

TEST_F(HttpProxyHttpsTest, WrongHostNameForTarget) {
	bool client_hit_header = false;

	http::ClientConfig client_config {
		.server_cert_path = "server.localhost.crt",
		.https_proxy = "http://localhost:" TEST_PROXY_PORT,
	};
	http::Client client(client_config, loop);
	auto req = make_shared<http::OutgoingRequest>();
	req->SetMethod(http::Method::GET);
	// Should not succeed with IP.
	req->SetAddress("https://127.0.0.1:" TEST_TLS_PORT "/index.html");
	auto err = client.AsyncCall(
		req,
		[&client_hit_header, this](http::ExpectedIncomingResponsePtr exp_resp) {
			EXPECT_FALSE(exp_resp);
			client_hit_header = true;
			loop.Stop();
		},
		[](http::ExpectedIncomingResponsePtr exp_resp) {
			EXPECT_TRUE(false) << "Should not get here";
		});
	ASSERT_EQ(error::NoError, err);

	loop.Run();

	EXPECT_TRUE(client_hit_header);
}

TEST_F(HttpProxyHttpsTest, WrongCertificate) {
	bool client_hit_header = false;

	http::ClientConfig client_config {
		.server_cert_path = "server.wrong.crt",
		.https_proxy = "http://localhost:" TEST_PROXY_PORT,
	};
	http::Client client(client_config, loop);
	auto req = make_shared<http::OutgoingRequest>();
	req->SetMethod(http::Method::GET);
	req->SetAddress("https://localhost:" TEST_TLS_PORT "/index.html");
	auto err = client.AsyncCall(
		req,
		[&client_hit_header, this](http::ExpectedIncomingResponsePtr exp_resp) {
			EXPECT_FALSE(exp_resp);
			client_hit_header = true;
			loop.Stop();
		},
		[](http::ExpectedIncomingResponsePtr exp_resp) {
			EXPECT_TRUE(false) << "Should not get here";
		});
	ASSERT_EQ(error::NoError, err);

	loop.Run();

	EXPECT_TRUE(client_hit_header);
}

TEST_F(HttpProxyHttpsTest, WrongTarget) {
	bool client_hit_header = false;
	bool client_hit_body = false;

	http::ClientConfig client_config {
		.server_cert_path = "server.localhost.crt",
		.https_proxy = "http://localhost:" TEST_PROXY_PORT,
	};
	http::Client client(client_config, loop);
	auto req = make_shared<http::OutgoingRequest>();
	req->SetMethod(http::Method::GET);
	req->SetAddress("https://localhost:" TEST_CLOSED_PORT "/index.html");
	auto err = client.AsyncCall(
		req,
		[&client_hit_header, this](http::ExpectedIncomingResponsePtr exp_resp) {
			ASSERT_FALSE(exp_resp);
			EXPECT_THAT(exp_resp.error().String(), testing::HasSubstr("500 Unable to connect"));
			client_hit_header = true;
			loop.Stop();
		},
		[&client_hit_body, this](http::ExpectedIncomingResponsePtr exp_resp) {
			ASSERT_TRUE(false) << "Should not get here";
			client_hit_body = true;
			loop.Stop();
		});
	ASSERT_EQ(error::NoError, err);

	loop.Run();

	EXPECT_TRUE(client_hit_header);
	EXPECT_FALSE(client_hit_body);
}

// HTTPS proxy with HTTP requests.
class HttpsProxyHttpTest : public HttpProxyTest {
public:
	void SetUp() override {
		StartPlainServer();
		StartTlsProxy();
	}
};

TEST_F(HttpsProxyHttpTest, BasicRequestAndResponse) {
	bool client_hit_header = false;
	bool client_hit_body = false;

	http::ClientConfig client_config {
		.server_cert_path = "server.localhost.crt",
		.http_proxy = "https://localhost:" TEST_TLS_PROXY_PORT,
	};
	http::Client client(client_config, loop);
	auto req = make_shared<http::OutgoingRequest>();
	req->SetMethod(http::Method::GET);
	req->SetAddress("http://127.0.0.1:" TEST_PORT "/index.html");
	vector<uint8_t> received;
	auto err = client.AsyncCall(
		req,
		[&client_hit_header, &received](http::ExpectedIncomingResponsePtr exp_resp) {
			ASSERT_TRUE(exp_resp) << exp_resp.error().String();
			auto resp = exp_resp.value();
			EXPECT_EQ(resp->GetStatusCode(), 200);
			client_hit_header = true;

			auto body_writer = make_shared<io::ByteWriter>(received);
			body_writer->SetUnlimited(true);
			resp->SetBodyWriter(body_writer);
		},
		[&client_hit_body, this](http::ExpectedIncomingResponsePtr exp_resp) {
			ASSERT_TRUE(exp_resp) << exp_resp.error().String();
			client_hit_body = true;
			loop.Stop();
		});
	ASSERT_EQ(error::NoError, err);

	loop.Run();

	EXPECT_TRUE(plain_server_hit_header);
	EXPECT_TRUE(plain_server_hit_body);
	EXPECT_TRUE(client_hit_header);
	EXPECT_TRUE(client_hit_body);
	EXPECT_EQ(common::StringFromByteVector(received), "Test\r\n");
}

TEST_F(HttpsProxyHttpTest, WrongProxySet) {
	bool client_hit_header = false;

	http::ClientConfig client_config {
		.server_cert_path = "server.localhost.crt",
		.http_proxy = "https://localhost:1",
	};
	http::Client client(client_config, loop);
	auto req = make_shared<http::OutgoingRequest>();
	req->SetMethod(http::Method::GET);
	req->SetAddress("http://localhost:" TEST_PORT "/index.html");
	auto err = client.AsyncCall(
		req,
		[&client_hit_header, this](http::ExpectedIncomingResponsePtr exp_resp) {
			EXPECT_FALSE(exp_resp);
			client_hit_header = true;
			loop.Stop();
		},
		[](http::ExpectedIncomingResponsePtr exp_resp) {
			EXPECT_TRUE(false) << "Should not get here";
		});
	ASSERT_EQ(error::NoError, err);

	loop.Run();

	EXPECT_TRUE(client_hit_header);
}

TEST_F(HttpsProxyHttpTest, WrongHostNameForProxy) {
	bool client_hit_header = false;

	http::ClientConfig client_config {
		.server_cert_path = "server.localhost.crt",
		// Should not succeed with IP.
		.http_proxy = "https://127.0.0.1:" TEST_TLS_PROXY_PORT,
	};
	http::Client client(client_config, loop);
	auto req = make_shared<http::OutgoingRequest>();
	req->SetMethod(http::Method::GET);
	req->SetAddress("http://localhost:" TEST_PORT "/index.html");
	auto err = client.AsyncCall(
		req,
		[&client_hit_header, this](http::ExpectedIncomingResponsePtr exp_resp) {
			EXPECT_FALSE(exp_resp);
			client_hit_header = true;
			loop.Stop();
		},
		[](http::ExpectedIncomingResponsePtr exp_resp) {
			EXPECT_TRUE(false) << "Should not get here";
		});
	ASSERT_EQ(error::NoError, err);

	loop.Run();

	EXPECT_TRUE(client_hit_header);
}

TEST_F(HttpsProxyHttpTest, WrongCertificate) {
	bool client_hit_header = false;

	http::ClientConfig client_config {
		.server_cert_path = "server.wrong.crt",
		.http_proxy = "https://localhost:" TEST_TLS_PROXY_PORT,
	};
	http::Client client(client_config, loop);
	auto req = make_shared<http::OutgoingRequest>();
	req->SetMethod(http::Method::GET);
	req->SetAddress("http://localhost:" TEST_PORT "/index.html");
	auto err = client.AsyncCall(
		req,
		[&client_hit_header, this](http::ExpectedIncomingResponsePtr exp_resp) {
			EXPECT_FALSE(exp_resp);
			client_hit_header = true;
			loop.Stop();
		},
		[](http::ExpectedIncomingResponsePtr exp_resp) {
			EXPECT_TRUE(false) << "Should not get here";
		});
	ASSERT_EQ(error::NoError, err);

	loop.Run();

	EXPECT_TRUE(client_hit_header);
}

TEST_F(HttpsProxyHttpTest, WrongTarget) {
	bool client_hit_header = false;
	bool client_hit_body = false;

	http::ClientConfig client_config {
		.server_cert_path = "server.localhost.crt",
		.http_proxy = "https://localhost:" TEST_TLS_PROXY_PORT,
	};
	http::Client client(client_config, loop);
	auto req = make_shared<http::OutgoingRequest>();
	req->SetMethod(http::Method::GET);
	req->SetAddress("http://localhost:" TEST_CLOSED_PORT "/index.html");
	auto err = client.AsyncCall(
		req,
		[&client_hit_header](http::ExpectedIncomingResponsePtr exp_resp) {
			ASSERT_TRUE(exp_resp) << exp_resp.error().String();
			auto resp = exp_resp.value();
			EXPECT_EQ(resp->GetStatusCode(), 500);
			EXPECT_EQ(resp->GetStatusMessage(), "Unable to connect");
			client_hit_header = true;
		},
		[&client_hit_body, this](http::ExpectedIncomingResponsePtr exp_resp) {
			client_hit_body = true;
			loop.Stop();
		});
	ASSERT_EQ(error::NoError, err);

	loop.Run();

	EXPECT_TRUE(client_hit_header);
	EXPECT_TRUE(client_hit_body);
}

// HTTPS proxy with HTTPS requests.
class HttpsProxyHttpsTest : public HttpProxyTest {
public:
	void SetUp() override {
		StartTlsServer();
		StartTlsProxy();
	}
};

TEST_F(HttpsProxyHttpsTest, BasicRequestAndResponse) {
	bool client_hit_header = false;
	bool client_hit_body = false;

	http::ClientConfig client_config {
		.server_cert_path = "server.localhost.crt",
		.https_proxy = "https://localhost:" TEST_TLS_PROXY_PORT,
	};
	http::Client client(client_config, loop);
	auto req = make_shared<http::OutgoingRequest>();
	req->SetMethod(http::Method::GET);
	req->SetAddress("https://localhost:" TEST_TLS_PORT "/index.html");
	vector<uint8_t> received;
	auto err = client.AsyncCall(
		req,
		[&client_hit_header, &received](http::ExpectedIncomingResponsePtr exp_resp) {
			ASSERT_TRUE(exp_resp) << exp_resp.error().String();
			auto resp = exp_resp.value();
			EXPECT_EQ(resp->GetStatusCode(), 200);
			client_hit_header = true;

			auto body_writer = make_shared<io::ByteWriter>(received);
			body_writer->SetUnlimited(true);
			resp->SetBodyWriter(body_writer);
		},
		[&client_hit_body, this](http::ExpectedIncomingResponsePtr exp_resp) {
			ASSERT_TRUE(exp_resp) << exp_resp.error().String();
			client_hit_body = true;
			loop.Stop();
		});
	ASSERT_EQ(error::NoError, err);

	loop.Run();

	EXPECT_TRUE(client_hit_header);
	EXPECT_TRUE(client_hit_body);
	EXPECT_EQ(common::StringFromByteVector(received), "Test\r\n");
}

TEST_F(HttpsProxyHttpsTest, WrongProxySet) {
	bool client_hit_header = false;

	http::ClientConfig client_config {
		.server_cert_path = "server.localhost.crt",
		.https_proxy = "https://localhost:1",
	};
	http::Client client(client_config, loop);
	auto req = make_shared<http::OutgoingRequest>();
	req->SetMethod(http::Method::GET);
	req->SetAddress("https://localhost:" TEST_TLS_PORT "/index.html");
	auto err = client.AsyncCall(
		req,
		[&client_hit_header, this](http::ExpectedIncomingResponsePtr exp_resp) {
			EXPECT_FALSE(exp_resp);
			client_hit_header = true;
			loop.Stop();
		},
		[](http::ExpectedIncomingResponsePtr exp_resp) {
			EXPECT_TRUE(false) << "Should not get here";
		});
	ASSERT_EQ(error::NoError, err);

	loop.Run();

	EXPECT_TRUE(client_hit_header);
}

TEST_F(HttpsProxyHttpsTest, WrongTarget) {
	bool client_hit_header = false;
	bool client_hit_body = false;

	http::ClientConfig client_config {
		.server_cert_path = "server.localhost.crt",
		.https_proxy = "https://localhost:" TEST_TLS_PROXY_PORT,
	};
	http::Client client(client_config, loop);
	auto req = make_shared<http::OutgoingRequest>();
	req->SetMethod(http::Method::GET);
	req->SetAddress("https://localhost:" TEST_CLOSED_PORT "/index.html");
	auto err = client.AsyncCall(
		req,
		[&client_hit_header, this](http::ExpectedIncomingResponsePtr exp_resp) {
			ASSERT_FALSE(exp_resp);
			EXPECT_THAT(exp_resp.error().String(), testing::HasSubstr("500 Unable to connect"));
			client_hit_header = true;
			loop.Stop();
		},
		[&client_hit_body, this](http::ExpectedIncomingResponsePtr exp_resp) {
			ASSERT_TRUE(false) << "Should not get here";
			client_hit_body = true;
			loop.Stop();
		});
	ASSERT_EQ(error::NoError, err);

	loop.Run();

	EXPECT_TRUE(client_hit_header);
	EXPECT_FALSE(client_hit_body);
}

TEST_F(HttpsProxyHttpsTest, WrongHostNameForProxy) {
	bool client_hit_header = false;

	http::ClientConfig client_config {
		.server_cert_path = "server.localhost.crt",
		// Should not succeed with IP.
		.https_proxy = "https://127.0.0.1:" TEST_TLS_PROXY_PORT,
	};
	http::Client client(client_config, loop);
	auto req = make_shared<http::OutgoingRequest>();
	req->SetMethod(http::Method::GET);
	req->SetAddress("https://localhost:" TEST_TLS_PORT "/index.html");
	auto err = client.AsyncCall(
		req,
		[&client_hit_header, this](http::ExpectedIncomingResponsePtr exp_resp) {
			EXPECT_FALSE(exp_resp);
			client_hit_header = true;
			loop.Stop();
		},
		[](http::ExpectedIncomingResponsePtr exp_resp) {
			EXPECT_TRUE(false) << "Should not get here";
		});
	ASSERT_EQ(error::NoError, err);

	loop.Run();

	EXPECT_TRUE(client_hit_header);
}

TEST_F(HttpsProxyHttpsTest, WrongHostNameForTarget) {
	bool client_hit_header = false;

	http::ClientConfig client_config {
		.server_cert_path = "server.localhost.crt",
		.https_proxy = "https://localhost:" TEST_TLS_PROXY_PORT,
	};
	http::Client client(client_config, loop);
	auto req = make_shared<http::OutgoingRequest>();
	req->SetMethod(http::Method::GET);
	// Should not succeed with IP.
	req->SetAddress("https://127.0.0.1:" TEST_TLS_PORT "/index.html");
	auto err = client.AsyncCall(
		req,
		[&client_hit_header, this](http::ExpectedIncomingResponsePtr exp_resp) {
			EXPECT_FALSE(exp_resp);
			client_hit_header = true;
			loop.Stop();
		},
		[](http::ExpectedIncomingResponsePtr exp_resp) {
			EXPECT_TRUE(false) << "Should not get here";
		});
	ASSERT_EQ(error::NoError, err);

	loop.Run();

	EXPECT_TRUE(client_hit_header);
}

TEST_F(HttpsProxyHttpsTest, WrongCertificate) {
	bool client_hit_header = false;

	http::ClientConfig client_config {
		.server_cert_path = "server.wrong.crt",
		.https_proxy = "https://localhost:" TEST_TLS_PROXY_PORT,
	};
	http::Client client(client_config, loop);
	auto req = make_shared<http::OutgoingRequest>();
	req->SetMethod(http::Method::GET);
	req->SetAddress("https://localhost:" TEST_TLS_PORT "/index.html");
	auto err = client.AsyncCall(
		req,
		[&client_hit_header, this](http::ExpectedIncomingResponsePtr exp_resp) {
			EXPECT_FALSE(exp_resp);
			client_hit_header = true;
			loop.Stop();
		},
		[](http::ExpectedIncomingResponsePtr exp_resp) {
			EXPECT_TRUE(false) << "Should not get here";
		});
	ASSERT_EQ(error::NoError, err);

	loop.Run();

	EXPECT_TRUE(client_hit_header);
}
