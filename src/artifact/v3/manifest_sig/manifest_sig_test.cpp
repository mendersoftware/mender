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

#include <artifact/v3/manifest_sig/manifest_sig.hpp>

#include <string>

#include <gtest/gtest.h>

using namespace std;

TEST(ParserTest, TestParseManifestSig) {
	std::string manifest_sig_data =
		R"(aec070645fe53ee3b3763059376134f058cc337247c978add178b6ccdfb0019f)";

	std::stringstream ss {manifest_sig_data};
	mender::common::io::StreamReader sr {ss};

	auto manifest_sig = mender::artifact::v3::manifest_sig::Parse(sr);

	ASSERT_TRUE(manifest_sig) << "error message: " << manifest_sig.error().message;

	auto manifest_sig_unwrapped = manifest_sig.value();

	ASSERT_EQ(
		manifest_sig_unwrapped, "aec070645fe53ee3b3763059376134f058cc337247c978add178b6ccdfb0019f")
		<< "ONE";
}
