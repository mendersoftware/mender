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
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDeviceIdentityGet(t *testing.T) {
	td := []struct {
		data string
		bad  bool
		ref  IdentityData
		code int
	}{
		{
			`
mac=123;123
bar=123
foo=bar
`,
			true,
			IdentityData{},
			1,
		},
		{
			`
foo=bar
key=value=23
some value=bar
mac=de:ad:be:ef:00:01
`,
			false,
			IdentityData{
				"foo":        "bar",
				"key":        "value=23",
				"some value": "bar",
				"mac":        "de:ad:be:ef:00:01",
			},
			0,
		},
		{
			"",
			true,
			IdentityData{},
			0,
		},
	}

	for _, tc := range td {
		// t.Logf("test data: %+v", tc)

		r := newTestOSCalls(tc.data, tc.code)
		ir := IdentityDataRunner{
			cmdr: &r,
		}
		id, err := ir.Get()

		if tc.bad {
			assert.Error(t, err)
		} else {
			assert.NoError(t, err)
			assert.NotEmpty(t, id)

			refdata, _ := json.Marshal(tc.ref)
			assert.Equal(t, string(refdata), id)
		}
	}
}

func TestDeviceIdentityParse(t *testing.T) {
	td := []struct {
		data string
		bad  bool
		ref  IdentityData
	}{
		{
			`
foo=bar
key=value=23
some value=bar
mac=de:ad:be:ef:00:01
`,
			false,
			IdentityData{
				"foo":        "bar",
				"key":        "value=23",
				"some value": "bar",
				"mac":        "de:ad:be:ef:00:01",
			},
		},
		{
			"",
			true,
			IdentityData{},
		},
	}

	for _, tc := range td {
		// t.Logf("test data: %+v", tc)

		id, err := parseIdentityData([]byte(tc.data))
		if tc.bad {
			assert.Error(t, err)
		} else {
			assert.NoError(t, err)
			assert.NotNil(t, id)

			val, ok := id.(*IdentityData)
			assert.True(t, ok)
			assert.Equal(t, tc.ref, *val)
		}
	}
}
