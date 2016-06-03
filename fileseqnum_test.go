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
	"math"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestFileSeqnum(t *testing.T) {
	ms := NewMemStore()

	fs := NewFileSeqnum("seqnum", ms)

	// make store disabled
	ms.Disable(true)
	v, err := fs.Get()
	assert.Error(t, err)
	ms.Disable(false)

	// Get should raise an error with read-only store
	ms.ReadOnly(true)
	v, err = fs.Get()
	assert.Error(t, err)
	ms.ReadOnly(false)

	// verify that value is generated correctly
	ms.WriteAll("seqnum", []byte("65535"))
	v, err = fs.Get()
	assert.NoError(t, err)
	assert.Equal(t, uint64(65536), v)

	d, err := ms.ReadAll("seqnum")
	assert.NoError(t, err)
	assert.Equal(t, "65536", string(d))

	ms.WriteAll("seqnum", []byte("foo"))
	v, err = fs.Get()
	assert.Error(t, err)

	// verify that the sequence starts from 1
	ms.Remove("seqnum")
	v, err = fs.Get()
	assert.NoError(t, err)
	assert.Equal(t, SeqnumStartVal, v)

	d, err = ms.ReadAll("seqnum")
	assert.NoError(t, err)
	assert.Equal(t, "1", string(d))

	// verify sequence wrap
	ms.WriteAll("seqnum", []byte(strconv.FormatUint(math.MaxUint64, 10)))
	v, err = fs.Get()
	assert.NoError(t, err)
	assert.Equal(t, SeqnumStartVal, v)

}
