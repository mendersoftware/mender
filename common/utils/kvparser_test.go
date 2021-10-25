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
package utils

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestKeyValParser(t *testing.T) {
	td := []struct {
		data string
		bad  bool
		ref  map[string][]string
	}{
		{
			`
foo=bar
key=value=23
some value=bar
mac=de:ad:be:ef:00:01
`,
			false,
			map[string][]string{
				"foo":        []string{"bar"},
				"key":        []string{"value=23"},
				"some value": []string{"bar"},
				"mac":        []string{"de:ad:be:ef:00:01"},
			},
		},
		{
			`
foo=bar
key=value=23
some value=bar
mac=de:ad:be:ef:00:01
foo=baz
`,
			false,
			map[string][]string{
				"foo":        []string{"bar", "baz"},
				"key":        []string{"value=23"},
				"some value": []string{"bar"},
				"mac":        []string{"de:ad:be:ef:00:01"},
			},
		},
		{
			`
foo=bar
mac
foo=baz
`,
			true,
			nil,
		},
	}

	for id, tc := range td {
		t.Logf("testing case: %+v\n", id)

		p := KeyValParser{}
		in := bytes.NewBuffer([]byte(tc.data))
		err := p.Parse(in)
		if tc.bad {
			assert.Error(t, err)
		} else {
			assert.NoError(t, err)
			data := p.Collect()
			assert.NotNil(t, data)

			assert.Len(t, data, len(tc.ref))
			for k, v := range tc.ref {
				if assert.Contains(t, data, k) {
					dv := data[k]
					for _, val := range v {
						assert.Contains(t, dv, val)
					}
				}
			}
		}
	}
}
