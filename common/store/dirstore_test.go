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
package store

import (
	"fmt"
	"io/ioutil"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

func pathExists(path string) bool {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return false
	}
	return true
}

func TestDirStore(t *testing.T) {

	tmppath, err := ioutil.TempDir("", "mendertest-")
	assert.NoError(t, err)
	defer os.RemoveAll(tmppath)

	d := NewDirStore(tmppath)

	// no file, should fail
	_, err = d.ReadAll("foo")
	assert.Error(t, err)
	assert.True(t, os.IsNotExist(err))

	var data string
	// do write/read cycle with changing data
	for i := 0; i < 2; i++ {
		data = fmt.Sprintf("foobar-%v", i)
		err := d.WriteAll("foo", []byte(data))
		assert.NoError(t, err)

		osi, err := os.Open(d.getPath("foo"))
		assert.NoError(t, err)
		indata, err := ioutil.ReadAll(osi)
		osi.Close()
		assert.NoError(t, err)
		assert.Equal(t, []byte(data), indata)
	}

	// read data written in last iteration of read/write cycle
	_, err = d.ReadAll("foo")
	assert.NoError(t, err)
	osi, err := os.Open(d.getPath("foo"))
	assert.NoError(t, err)
	indata, err := ioutil.ReadAll(osi)
	osi.Close()
	assert.NoError(t, err)
	assert.Equal(t, []byte(data), indata)

	// not the same using io.ReadCloser interface
	in, err := d.OpenRead("foo")
	assert.NoError(t, err)
	indata, err = ioutil.ReadAll(in)
	in.Close()

	assert.NoError(t, err)
	assert.Equal(t, []byte(data), indata)

	// check writer
	out, err := d.OpenWrite("bar")
	assert.NoError(t, err)
	// there should be a temp path already, but the actual target path should not
	// exist yet
	assert.True(t, pathExists(d.getTempPath("bar")))
	assert.False(t, pathExists(d.getPath("bar")))

	_, err = out.Write([]byte("zed"))
	assert.NoError(t, err)
	// the same
	assert.True(t, pathExists(d.getTempPath("bar")))
	assert.False(t, pathExists(d.getPath("bar")))
	out.Close()

	// commit the file now
	out.Commit()
	assert.False(t, pathExists(d.getTempPath("bar")))
	assert.True(t, pathExists(d.getPath("bar")))

	err = d.Remove("bar")
	assert.NoError(t, err)
	assert.False(t, pathExists(d.getPath("bar")))

	err = d.Remove("foobar")
	assert.True(t, os.IsNotExist(err))

	// closing is a noop, no errors should be reported
	err = d.Close()
	assert.NoError(t, err)

	// Test reading from an absolute path
	tf, err := ioutil.TempFile(tmppath, "abspathtest")
	assert.NoError(t, err)
	defer os.Remove(tf.Name())
	_, err = d.OpenRead(tf.Name())
	assert.NoError(t, err)
}
