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

#include <artifact/v3/manifest/manifest.hpp>

#include <string>

#include <gtest/gtest.h>

#include <common/log.hpp>

using namespace std;

TEST(ParserTest, TestParseManifest) {
	std::string manifest_data =
		R"(aec070645fe53ee3b3763059376134f058cc337247c978add178b6ccdfb0019f  data/0000.tar
9f65db081a46f7832b9767c56afcc7bfe784f0a62cc2950b6375b2b6390e6e50  header.tar
96bcd965947569404798bcbdb614f103db5a004eb6e364cfc162c146890ea35b  version
)";

	std::stringstream ss {manifest_data};

	mender::common::io::StreamReader sr {ss};

	auto manifest = mender::artifact::v3::manifest::Parse(sr);

	ASSERT_TRUE(manifest) << "error message: " << manifest.error().message;

	auto manifest_unwrapped = manifest.value();

	EXPECT_EQ(
		manifest_unwrapped.Get("version"),
		"96bcd965947569404798bcbdb614f103db5a004eb6e364cfc162c146890ea35b")
		<< "ONE";
	EXPECT_EQ(
		manifest_unwrapped.Get("header.tar"),
		"9f65db081a46f7832b9767c56afcc7bfe784f0a62cc2950b6375b2b6390e6e50")
		<< "TWO";
	EXPECT_EQ(
		manifest_unwrapped.Get("data/0000.tar"),
		"aec070645fe53ee3b3763059376134f058cc337247c978add178b6ccdfb0019f")
		<< "THREE";
	EXPECT_EQ(manifest_unwrapped.Get("IDoNotExist"), "");

	// Check the checksum of the whole manifest
	EXPECT_EQ(
		manifest_unwrapped.GetShaSum().String(),
		"cbea329fa8ae6223656b8c96015c41313cd6e7a199400ea6854b0a653052802d")
		<< "SHASUM";
}

TEST(ParserTest, TestParseManifestFormatErrorShasumLength) {
	/* Two characters missing from the shasum */
	std::string manifest_data =
		R"(aec070645fe53ee3b3763059376134f058cc337247c978add178b6ccdfb001  data/0000.tar
)";

	std::stringstream ss {manifest_data};

	mender::common::io::StreamReader sr {ss};

	auto manifest = mender::artifact::v3::manifest::Parse(sr);

	EXPECT_FALSE(manifest) << manifest.error().message;

	EXPECT_EQ(
		manifest.error().message,
		"Line (aec070645fe53ee3b3763059376134f058cc337247c978add178b6ccdfb001  data/0000.tar) is not in the expected manifest format: ^([0-9a-z]{64})[[:space:]]{2}([^[:blank:]]+)$");
}

TEST(ParserTest, TestParseManifestFormatErrorMissingName) {
	std::string manifest_data =
		R"(96bcd965947569404798bcbdb614f103db5a004eb6e364cfc162c146890ea35b
)";

	std::stringstream ss {manifest_data};

	mender::common::io::StreamReader sr {ss};

	auto manifest = mender::artifact::v3::manifest::Parse(sr);

	EXPECT_FALSE(manifest) << manifest.error().message;

	EXPECT_EQ(
		manifest.error().message,
		"Line (96bcd965947569404798bcbdb614f103db5a004eb6e364cfc162c146890ea35b) is not in the expected manifest format: ^([0-9a-z]{64})[[:space:]]{2}([^[:blank:]]+)$");
}


TEST(ParserTest, TestParseManifestFormatErrorWrongNumberOfWhitespaceSeparators) {
	/* 3 instead of two spaces in between the sha and the name */
	std::string manifest_data =
		R"(96bcd965947569404798bcbdb614f103db5a004eb6e364cfc162c146890ea35b   version
)";

	std::stringstream ss {manifest_data};

	mender::common::io::StreamReader sr {ss};

	auto manifest = mender::artifact::v3::manifest::Parse(sr);

	EXPECT_FALSE(manifest);

	EXPECT_EQ(
		manifest.error().message,
		"Line (96bcd965947569404798bcbdb614f103db5a004eb6e364cfc162c146890ea35b   version) is not in the expected manifest format: ^([0-9a-z]{64})[[:space:]]{2}([^[:blank:]]+)$");
}

TEST(ParserTest, TestParseManifestFormatErrorAllOnOneLine) {
	std::string manifest_data =
		R"(aec070645fe53ee3b3763059376134f058cc337247c978add178b6ccdfb0019f  data/00 00.tar
9f65db081a46f7832b9767c56afcc7bfe784f0a62cc2950b6375b2b6390e6e50  header.tar
96bcd965947569404798bcbdb614f103db5a004eb6e364cfc162c146890ea35b  version)";

	std::stringstream ss {manifest_data};

	mender::common::io::StreamReader sr {ss};

	auto manifest = mender::artifact::v3::manifest::Parse(sr);

	EXPECT_FALSE(manifest);

	EXPECT_EQ(
		manifest.error().message,
		"Line (aec070645fe53ee3b3763059376134f058cc337247c978add178b6ccdfb0019f  data/00 00.tar) is not in the expected manifest format: ^([0-9a-z]{64})[[:space:]]{2}([^[:blank:]]+)$");
}

TEST(ParserTest, TestParseManifestFormatErrorNewlineSeparators) {
	std::string manifest_data =
		R"(aec070645fe53ee3b3763059376134f058cc337247c978add178b6ccdfb0019f data/0000.tar 9f65db081a46f7832b9767c56afcc7bfe784f0a62cc2950b6375b2b6390e6e50 header.tar 96bcd965947569404798bcbdb614f103db5a004eb6e364cfc162c146890ea35b version)";

	std::stringstream ss {manifest_data};

	mender::common::io::StreamReader sr {ss};

	auto manifest = mender::artifact::v3::manifest::Parse(sr);

	EXPECT_FALSE(manifest) << manifest.error().message << std::endl;

	EXPECT_EQ(
		manifest.error().message,
		"Line (aec070645fe53ee3b3763059376134f058cc337247c978add178b6ccdfb0019f data/0000.tar 9f65db081a46f7832b9767c56afcc7bfe784f0a62cc2950b6375b2b6390e6e50 header.tar 96bcd965947569404798bcbdb614f103db5a004eb6e364cfc162c146890ea35b version) is not in the expected manifest format: ^([0-9a-z]{64})[[:space:]]{2}([^[:blank:]]+)$");
}

TEST(ParserTest, TestParseManifestFormatStripCompressionSuffixes) {
	/* Two characters missing from the shasum */
	std::string manifest_data =
		R"(aec070645fe53ee3b3763059376134f058cc337247c978add178b6ccdfb0019f  data/0000.tar.xz
aec070645fe53ee3b3763059376134f058cc337247c978add178b6ccdfb0019f  manifest.zst
9f65db081a46f7832b9767c56afcc7bfe784f0a62cc2950b6375b2b6390e6e50  header.tar.gz
96bcd965947569404798bcbdb614f103db5a004eb6e364cfc162c146890ea35b  version
)";

	std::stringstream ss {manifest_data};

	mender::common::io::StreamReader sr {ss};

	auto manifest = mender::artifact::v3::manifest::Parse(sr);

	ASSERT_TRUE(manifest) << "error message: " << manifest.error().message;

	auto manifest_unwrapped = manifest.value();

	ASSERT_EQ(
		manifest_unwrapped.Get("data/0000.tar"),
		"aec070645fe53ee3b3763059376134f058cc337247c978add178b6ccdfb0019f");

	ASSERT_EQ(
		manifest_unwrapped.Get("manifest"),
		"aec070645fe53ee3b3763059376134f058cc337247c978add178b6ccdfb0019f");

	ASSERT_EQ(
		manifest_unwrapped.Get("header.tar"),
		"9f65db081a46f7832b9767c56afcc7bfe784f0a62cc2950b6375b2b6390e6e50");
}
