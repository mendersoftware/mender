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
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestTempInventoryData(t *testing.T) {
	td := tempInventoryData{}

	td.Add("foo", "bar")
	assert.Contains(t, td, "foo")
	assert.Equal(t, tempInventoryAttribute{"bar"}, td["foo"])

	fattrs := td["foo"]
	assert.Equal(t, InventoryAttribute{"foo", "bar"},
		fattrs.ToInventoryAttribute("foo"))

	td.Add("foo", "baz")
	assert.Contains(t, td, "foo")
	assert.Equal(t, tempInventoryAttribute{"bar", "baz"}, td["foo"])

	fattrs = td["foo"]
	assert.Equal(t, InventoryAttribute{"foo", []string{"bar", "baz"}},
		fattrs.ToInventoryAttribute("foo"))

	tdnew := tempInventoryData{
		"foo": []string{"zed"},
		"zed": []string{"zen"},
	}

	// inventory data should get merged
	td.Append(tdnew)

	expecttd := tempInventoryData{
		"foo": tempInventoryAttribute{"bar", "baz", "zed"},
		"zed": tempInventoryAttribute{"zen"},
	}
	assert.Equal(t, expecttd, td)

	idata := td.ToInventoryData()
	// for each InventoryAttribute check that it exists in expected set and that
	// it has an equal value
	for _, idi := range idata {
		if assert.Contains(t, expecttd, idi.Name, expecttd) {
			expv := expecttd[idi.Name]
			if len(expv) > 1 {
				if assert.IsType(t, []string{}, idi.Value) {
					ls, _ := idi.Value.([]string)
					assert.Len(t, ls, len(expv))
					for _, v := range ls {
						assert.Contains(t, expv, v)
					}
				}
			} else if len(expv) == 1 {
				assert.Equal(t, expv[0], idi.Value)
			}
		}
	}
}

func TestParseInventoryData(t *testing.T) {
	td, err := parseInventoryData([]byte(`
foo=bar
foo=baz
zed=zen
`))
	assert.NoError(t, err)
	assert.NotNil(t, td)
	assert.Equal(t, tempInventoryData{
		"foo": tempInventoryAttribute{"bar", "baz"},
		"zed": tempInventoryAttribute{"zen"},
	}, td)

	_, err = parseInventoryData([]byte(``))
	assert.EqualError(t, err, "obtained no output")

}
