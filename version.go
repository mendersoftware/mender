// Copyright 2016 Mender Software AS
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
package main

var (
	// The commit that the current build.
	Commit string

	// If the current build for a tag, this includes the tag’s name.
	Tag string

	// For builds not triggered by a pull request this is the name of the branch
	// currently being built; whereas for builds triggered by a pull request
	// this is the name of the branch targeted by the pull request
	// (in many cases this will be master).
	Branch string

	// The number of the current build (for example, “4”).
	BuildNumber string
)

func CreateVersionString() string {

	switch {
	case Tag != "":
		return Tag

	case Commit != "" && Branch != "":
		return Branch + "_" + Commit
	}

	return "unknown"
}
