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
package device

import (
	"encoding/json"
	"testing"

	stest "github.com/mendersoftware/mender/common/system/testing"
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
			`
foo=bar
foo=baz
key=value=23
some value=bar
mac=de:ad:be:ef:00:01
`,
			false,
			IdentityData{
				"foo":        []string{"bar", "baz"},
				"key":        "value=23",
				"some value": "bar",
				"mac":        "de:ad:be:ef:00:01",
			},
			0,
		},
		{
			`
foo=bar
foo=baz
keyvalue
`,
			true,
			nil,
			0,
		},
		{
			"",
			true,
			IdentityData{},
			0,
		},
	}

	for id, tc := range td {
		t.Logf("test case: %+v", id)

		r := stest.NewTestOSCalls(tc.data, tc.code)
		ir := IdentityDataRunner{
			Cmdr: r,
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
