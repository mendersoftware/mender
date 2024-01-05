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

#include <common/crypto.hpp>
#include <artifact/sha/sha.hpp>

#include <string>
#include <vector>

#include <gmock/gmock.h>
#include <gtest/gtest.h>

#include <common/testing.hpp>
#include <common/path.hpp>

using namespace std;

namespace mtesting = mender::common::testing;
using testing::HasSubstr;

namespace error = mender::common::error;
namespace path = mender::common::path;

namespace mender {
namespace common {
namespace crypto {

TEST(CryptoTest, TestSignRSA) {
	string data_ {"foobar"};
	vector<uint8_t> testdata {data_.begin(), data_.end()};
	string private_key_file = "./private-key.rsa.pem";
	auto expected_signature = crypto::Sign({private_key_file}, testdata);
	ASSERT_TRUE(expected_signature) << "Unexpected: " << expected_signature.error();
	EXPECT_EQ(
		expected_signature.value(),
		"E25EpWIT4LaVi0AUKCFxPuSDB+jk6HcSOnTMywgKqhxnPAC/MObbK24rMT97zVe+17ldQEszpyT04YLxEN8J9lJiJ48yJnU6A6iQ0GW2i6q0ximATal+l2RkKs22Ub5/MDV6UOeZlxska8C3PST2Cj4yNJ3r6ZvRqAb+3RhFKCPw9pR1nyD8agTwxzFBg5ejoQmm+5xy/hyf9kyNJKmIp2SxJERym8Tfc95a9UtvbPSkB2Hxk8yfwqzyxjourcZRbXgOJvbaJCSHHrEmN7siVPTA+dQPfnCvLJtRN6nboPMEpbA89Uv/n9TyIkT4iWhNCkAfhlbUUexpUafb9zcXjYSFtq6IENIIgr8fyYkhlbPpnhNYjtPQ1McfMDDWc4MB/CNZYGGGzAjnF4UqozeSe8bIRNX6Q6t1wPK+32lgjklq3GSwFo20/wP1WvBHNN6jc5wQfoCecRfEdB3Y2CMQysEilpPR4wDreRI86dQt5mLqUF9tP2QfuFOHYjpDQZ0w");
}


TEST(CryptoTest, TestSignED25519) {
	string data_ {"foobar"};
	vector<uint8_t> testdata {data_.begin(), data_.end()};
	string private_key_file = "./client.1.ed25519.key";
	auto expected_signature = crypto::Sign({private_key_file}, testdata);
	ASSERT_TRUE(expected_signature) << "Unexpected: " << expected_signature.error();
	EXPECT_EQ(
		expected_signature.value(),
		"J3yvQQfMzqAQ1fXo9fKhiCtOJCndksaTpLU0Vcnt69RRZ1juu7Bdw3m8mq7gzXxm0WvHvAX9riuTP/7Sw1stAA==");
}

TEST(CryptoTest, TestKeyFileNotFound) {
	string private_key_file = "./i-do-not-exist.pem";
	auto expected_signature = crypto::Sign({private_key_file}, {});
	ASSERT_FALSE(expected_signature);
	EXPECT_THAT(
		expected_signature.error().message,
		::testing::StartsWith("Failed to load the private key"));
}

TEST(CryptoTest, TestPublicKeyExtraction) {
	string private_key_file = "./private-key.rsa.pem";
	auto expected_public_key = crypto::ExtractPublicKey({private_key_file});
	ASSERT_TRUE(expected_public_key) << "Unexpected: " << expected_public_key.error();
	EXPECT_EQ(
		expected_public_key.value(),
		"-----BEGIN PUBLIC KEY-----\nMIIBojANBgkqhkiG9w0BAQEFAAOCAY8AMIIBigKCAYEAmNXA6xtQoKiZe1Z9DlX+\nW4pubQsj+R3GDKx9Wmgd91N28hMhq/1Z9JGlIp4JbBYyWgiHBSFRo/6XefMrIIiL\nhS0Z8RPkWo20JhNEYTNx6BbkWoPVuKNMZB9iN5kx28t+ptAEuSRAZUFqBTWHfXr9\n+Yy4F5cRJFvALYgobUHx5dKXscItuiLG03ll3taz4/CCRQI5Lp0ZmJE+q4dUJ4h7\nfsLtrDGoQj3sRpPPIJPTnLAMMise3ZBUEfzAoQ7Yw1Crap51oGzal9/9xxAqDxyo\nt/t416ItybRG9VMS721txbDm7I9TIEBVpe6OOuKTEK2HA1vTcwlAGEJxJ+7kcFxx\neKltfHSOhKtxGZGg+fP/JNe42GKRf5YsvXciG/qnmRVRoN1l9HmzSvx5daEOOccJ\n4blUsskfAFJ2oro8RqWvA1elxdqH2gcfYxQgTXudntl1KHaCbeDzj++wxMMSe9LM\niLeCNI59lkRH00f4CEj3DcHoxfRV5Dr/H6Xxtu7boLS7AgMBAAE=\n-----END PUBLIC KEY-----\n");
}


TEST(CryptoTest, TestPublicKeyExtractionED25519) {
	string private_key_file = "./client.1.ed25519.key";
	auto expected_public_key = crypto::ExtractPublicKey({private_key_file});
	ASSERT_TRUE(expected_public_key) << "Unexpected: " << expected_public_key.error();
	EXPECT_EQ(
		expected_public_key.value(),
		"-----BEGIN PUBLIC KEY-----\nMCowBQYDK2VwAyEACZyvqjmSx+pU1i8IBO5VHJ9gEZ+XG1JwefboZiiye1c=\n-----END PUBLIC KEY-----\n");
}

TEST(CryptoTest, TestPublicKeyExtractionError) {
	string private_key_file = "./i-do-not-exist.pem";
	auto expected_public_key = crypto::ExtractPublicKey({private_key_file});
	ASSERT_FALSE(expected_public_key);
	EXPECT_THAT(
		expected_public_key.error().message,
		::testing::StartsWith("Failed to load the private key"));
}

TEST(CryptoTest, TestEncodeDecodeBase64) {
	vector<uint8_t> testdata {1, 2, 3, 4, 5, 6, 7, 8, 9, 255};

	auto expected_encoded = crypto::EncodeBase64(testdata);
	ASSERT_TRUE(expected_encoded) << "Unexpected: " << expected_encoded.error();
	EXPECT_EQ(expected_encoded.value(), "AQIDBAUGBwgJ/w==");

	string encoded_data_ {"AQIDBAUGBwgJ/w=="};
	auto expected_decoded = crypto::DecodeBase64(encoded_data_);
	ASSERT_TRUE(expected_decoded) << "Unexpected: " << expected_decoded.error();
	EXPECT_THAT(
		expected_decoded.value(), ::testing::ElementsAreArray({1, 2, 3, 4, 5, 6, 7, 8, 9, 255}));
}

TEST(CryptoTest, TestVerifySignValidRSA) {
	string data_ {"foobar"};
	vector<uint8_t> testdata {data_.begin(), data_.end()};
	string private_key_file = "./private-key.rsa.pem";
	auto expected_signature = crypto::Sign({private_key_file}, testdata);
	ASSERT_TRUE(expected_signature) << "Unexpected: " << expected_signature.error();

	auto signature = expected_signature.value();

	auto expected_shasum = mender::sha::Shasum(testdata);
	ASSERT_TRUE(expected_shasum) << "Unexpected: " << expected_shasum.error();
	string public_key_file = "./public-key.rsa.pem";
	auto expected_verify_signature =
		crypto::VerifySign(public_key_file, expected_shasum.value(), signature);
	ASSERT_TRUE(expected_verify_signature) << "Unexpected: " << expected_verify_signature.error();
	ASSERT_TRUE(expected_verify_signature.value());
}

TEST(CryptoTest, TestVerifySignValidECDSA) {
	string data_ {"foobar"};
	vector<uint8_t> testdata {data_.begin(), data_.end()};
	string private_key_file = "./private-key.ecdsa.pem";
	auto expected_signature = crypto::Sign({private_key_file}, testdata);
	ASSERT_TRUE(expected_signature) << "Unexpected: " << expected_signature.error();

	auto signature = expected_signature.value();

	cout << "Got signature: " << signature << endl;

	auto expected_shasum = mender::sha::Shasum(testdata);
	ASSERT_TRUE(expected_shasum) << "Unexpected: " << expected_shasum.error();
	string public_key_file = "./public-key.ecdsa.pem";
	auto expected_verify_signature =
		crypto::VerifySign(public_key_file, expected_shasum.value(), signature);
	ASSERT_TRUE(expected_verify_signature) << "Unexpected: " << expected_verify_signature.error();
	ASSERT_TRUE(expected_verify_signature.value());
}

TEST(CryptoTest, TestVerifySignInvalid) {
	string data_ {"foobar"};
	string public_key_file = "./public-key.rsa.pem";

	vector<uint8_t> testdata {data_.begin(), data_.end()};
	auto expected_shasum = mender::sha::Shasum(testdata);
	ASSERT_TRUE(expected_shasum) << "Unexpected: " << expected_shasum.error();
	mender::sha::SHA shasum = expected_shasum.value();


	// Wrong length
	string short_signature_encoded = "AQIDBAUGBwgJ/w==";
	auto expected_verify_signature =
		crypto::VerifySign(public_key_file, shasum, short_signature_encoded);
	ASSERT_TRUE(expected_verify_signature);
	ASSERT_FALSE(expected_verify_signature.value());

	// Modified signature
	string bad_signature_encoded =
		"E25EpWIT4LaVi0AUKCFxPuSDB+jk6HcSOnTMywgKqiBnPAC/MObbK24rMT97zVe+17ldQEszpyT04YLxEN8J9lJiJ48yJnU6A6iQ0GW2i6q0ximATal+l2RkKs22Ub5/MDV6UOeZlxska8C3PST2Cj4yNJ3r6ZvRqAb+3RhFKCPw9pR1nyD8agTwxzFBg5ejoQmm+5xy/hyf9kyNJKmIp2SxJERym8Tfc95a9UtvbPSkB2Hxk8yfwqzyxjourcZRbXgOJvbaJCSHHrEmN7siVPTA+dQPfnCvLJtRN6nboPMEpbA89Uv/n9TyIkT4iWhNCkAfhlbUUexpUafb9zcXjYSFtq6IENIIgr8fyYkhlbPpnhNYjtPQ1McfMDDWc4MB/CNZYGGGzAjnF4UqozeSe8bIRNX6Q6t1wPK+32lgjklq3GSwFo20/wP1WvBHNN6jc5wQfoCecRfEdB3Y2CMQysEilpPR4wDreRI86dQt5mLqUF9tP2QfuFOHYjpDQZ0w";
	expected_verify_signature = crypto::VerifySign(public_key_file, shasum, bad_signature_encoded);
	ASSERT_TRUE(expected_verify_signature);
	ASSERT_FALSE(expected_verify_signature.value());

	// Inexisting key
	string good_signature_encoded =
		"E25EpWIT4LaVi0AUKCFxPuSDB+jk6HcSOnTMywgKqhxnPAC/MObbK24rMT97zVe+17ldQEszpyT04YLxEN8J9lJiJ48yJnU6A6iQ0GW2i6q0ximATal+l2RkKs22Ub5/MDV6UOeZlxska8C3PST2Cj4yNJ3r6ZvRqAb+3RhFKCPw9pR1nyD8agTwxzFBg5ejoQmm+5xy/hyf9kyNJKmIp2SxJERym8Tfc95a9UtvbPSkB2Hxk8yfwqzyxjourcZRbXgOJvbaJCSHHrEmN7siVPTA+dQPfnCvLJtRN6nboPMEpbA89Uv/n9TyIkT4iWhNCkAfhlbUUexpUafb9zcXjYSFtq6IENIIgr8fyYkhlbPpnhNYjtPQ1McfMDDWc4MB/CNZYGGGzAjnF4UqozeSe8bIRNX6Q6t1wPK+32lgjklq3GSwFo20/wP1WvBHNN6jc5wQfoCecRfEdB3Y2CMQysEilpPR4wDreRI86dQt5mLqUF9tP2QfuFOHYjpDQZ0w";
	expected_verify_signature =
		crypto::VerifySign("non-existing.key", shasum, good_signature_encoded);
	ASSERT_FALSE(expected_verify_signature);
	EXPECT_THAT(expected_verify_signature.error().message, HasSubstr("No such file or directory"));
}

TEST(CryptoTest, TestPrivateKeyLoadFromPEMValidRSA) {
	// Load RSA private key from PEM
	string private_key_file = "./private-key.rsa.pem";
	auto expected_private_key = PrivateKey::Load({private_key_file});
	ASSERT_TRUE(expected_private_key) << "Unexpected: " << expected_private_key.error();
}

TEST(CryptoTest, TestPrivateKeyLoadFromPEMValidECDSA) {
	// Load ECDSA private key from PEM
	string private_key_file = "./private-key.ecdsa.pem";
	auto expected_private_key = PrivateKey::Load({private_key_file});
	ASSERT_TRUE(expected_private_key) << "Unexpected: " << expected_private_key.error();
}

TEST(CryptoTest, TestPrivateKeyLoadFromPEMFileNotFound) {
	// Load non-exsistent private key from PEM
	string private_key_file = "./private-non-existent.pem";
	auto expected_private_key = PrivateKey::Load({private_key_file});
	EXPECT_THAT(expected_private_key.error().message, HasSubstr("No such file or directory"));
}

TEST(CryptoTest, TestPrivateKeyLoadFromPEMInvalid) {
	// Load corrupted/unsupported private key from PEM
	string private_key_file = "./private-corrupted.pem";
	auto expected_private_key = PrivateKey::Load({private_key_file});
	EXPECT_THAT(expected_private_key.error().message, HasSubstr("Failed to load the private key"));
}

TEST(CryptoTest, TestPrivateKeyLoadFromPEMNoPassphrase) {
	// Load encrypted private key with no password
	string private_key_file = "./private-encrypted.pem";
	auto expected_private_key = PrivateKey::Load({private_key_file});
	EXPECT_THAT(expected_private_key.error().message, HasSubstr("Failed to load the private key"));
}

TEST(CryptoTest, TestPrivateKeyLoadFromPEMWrongPassphrase) {
	// Load encrypted private key with wrong password
	string private_key_file = "./private-encrypted.pem";
	string passphrase = "dunno";
	auto expected_private_key = PrivateKey::Load({private_key_file, passphrase});
	EXPECT_THAT(expected_private_key.error().message, HasSubstr("Failed to load the private key"));
}

TEST(CryptoTest, TestPrivateKeyLoadFromPEMCorrectPassphrase) {
	// Load encrypted private key with correct password
	string private_key_file = "./private-encrypted.pem";
	string passphrase = "secret";
	auto expected_private_key = PrivateKey::Load({private_key_file, passphrase});
	ASSERT_TRUE(expected_private_key) << "Unexpected: " << expected_private_key.error();
}

TEST(CryptoTest, TestPrivateKeyGenerate) {
	auto expected_private_key = PrivateKey::Generate(3072);
	EXPECT_TRUE(expected_private_key) << "Unexpected: " << expected_private_key.error();

	auto expected_private_key_exponent = PrivateKey::Generate(3072, 65539);
	EXPECT_TRUE(expected_private_key_exponent)
		<< "Unexpected: " << expected_private_key_exponent.error();
}

TEST(CryptoTest, TestPrivateKeySaveToPEM) {
	string private_key_file = "./private-key.rsa.traditional.pem";
	auto expected_private_key = PrivateKey::Load({private_key_file});
	ASSERT_TRUE(expected_private_key) << "Unexpected: " << expected_private_key.error();
	auto private_key = std::move(expected_private_key.value());

	mtesting::TemporaryDirectory tmpdir;
	string tmpfile = path::Join(tmpdir.Path(), "private.key");
	auto err = private_key.SaveToPEM(tmpfile);
	EXPECT_EQ(error::NoError, err);

	EXPECT_TRUE(mtesting::FilesEqual(private_key_file, tmpfile));
}

} // namespace crypto
} // namespace common
} // namespace mender
