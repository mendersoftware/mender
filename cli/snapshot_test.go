// Copyright 2020 Northern.tech AS
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
package cli

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestWrongCompressionArgs(t *testing.T) {
	var err error

	err = SetupCLI([]string{"mender", "snapshot", "dump", "-C", "lzma"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not implemented")

	err = SetupCLI([]string{"mender", "snapshot", "dump", "-C", "abc"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "Unknown compression")
}
