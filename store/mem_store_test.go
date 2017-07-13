// Copyright 2017 Northern.tech AS
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

package store

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMemStore(t *testing.T) {
	testKey := "foo"
	testValue := []byte("bar")

	ms := NewMemStore()
	err := ms.WriteAll(testKey, testValue)
	assert.NoError(t, err)

	readKey, err := ms.ReadAll(testKey)
	assert.NoError(t, err)
	assert.Equal(t, testValue, readKey)

	err = ms.Remove(testKey)
	assert.NoError(t, err)

	readKey, err = ms.ReadAll(testKey)
	assert.Empty(t, readKey)
	assert.EqualError(t, err, os.ErrNotExist.Error())

	err = ms.WriteAll(testKey, testValue)
	assert.NoError(t, err)

	ms.Disable(true)

	err = ms.WriteAll("test", testValue)
	assert.EqualError(t, err, errDisabled.Error())

	ms.Disable(false)

	err = ms.WriteAll("test", testValue)
	assert.NoError(t, err)

	err = ms.Close()
	assert.NoError(t, err)
}
