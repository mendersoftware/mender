// Copyright 2021 Northern.tech AS
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
package api

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUnMarshalErrorMessage(t *testing.T) {
	errData := new(struct {
		Error string `json:"error"`
	})

	jsonErrMsg := `
  {
    "error" : "failed to decode device group data: JSON payload is empty"
  }
`
	require.Nil(t, json.Unmarshal([]byte(jsonErrMsg), errData))

	expected := "failed to decode device group data: JSON payload is empty"
	assert.Equal(t, expected, unmarshalErrorMessage(bytes.NewReader([]byte(jsonErrMsg))))
}

func TestErrorUnmarshaling(t *testing.T) {

	tests := map[string]struct {
		input    string
		expected string
	}{
		"Regular JSON": {
			input: `{
    "error": "foobar"
}`,
			expected: "foobar",
		},
		"Simply an error string": {
			input:    "Error message from the server",
			expected: "Error message from the server",
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			res := unmarshalErrorMessage(strings.NewReader(test.input))
			assert.Equal(t, test.expected, res)
		})
	}

}
