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

import (
	"github.com/stretchr/testify/assert"
	"testing"
)

func TestVersionUnknown(t *testing.T) {
	Commit = ""
	Tag = ""
	Branch = ""
	BuildNumber = ""
	v := CreateVersionString()

	assert.Equal(t, "unknown", v)
}

func TestVersionTag(t *testing.T) {
	Commit = ""
	Tag = "foo"
	Branch = ""
	BuildNumber = ""
	v := CreateVersionString()

	assert.Equal(t, "foo", v)
}

func TestVersionBranchCommit(t *testing.T) {
	Commit = "foo"
	Tag = ""
	Branch = "baz"
	BuildNumber = ""
	v := CreateVersionString()

	assert.Equal(t, "baz_foo", v)
}

func TestVersionBranchCommitTag(t *testing.T) {
	Commit = "foo"
	Tag = "bar"
	Branch = "baz"
	BuildNumber = ""
	v := CreateVersionString()

	// tag takes priority over other settings
	assert.Equal(t, "bar", v)
}
