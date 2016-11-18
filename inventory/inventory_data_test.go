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
package inventory

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDataDecoder(t *testing.T) {

	idec := NewDataDecoder()
	assert.NotNil(t, idec)

	idec.appendFromRaw(map[string][]string{
		"foo": []string{"bar"},
	})

	assert.Contains(t, idec.GetData(), Attribute{"foo", "bar"})

	idec.appendFromRaw(map[string][]string{
		"foo": []string{"baz"},
	})
	assert.Contains(t, idec.data, "foo")
	assert.Contains(t, idec.GetData(),
		Attribute{"foo", []string{"bar", "baz"}})

	idec.appendFromRaw(map[string][]string{
		"bar": []string{"zen"},
	})
	assert.Contains(t, idec.GetData(),
		Attribute{"foo", []string{"bar", "baz"}})
	assert.Contains(t, idec.GetData(), Attribute{"bar", "zen"})

	idata := idec.GetData()
	assert.Len(t, idata, 2)
	assert.Contains(t, idata, Attribute{"foo", []string{"bar", "baz"}})
	assert.Contains(t, idata, Attribute{"bar", "zen"})
}
