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
package store

import (
	"fmt"
	"io/ioutil"
	"os"
	"path"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDBStore(t *testing.T) {

	d := &DBStore{}
	_, err := d.ReadAll("foo")
	assert.EqualError(t, err, ErrDBStoreNotInitialized.Error())

	err = d.WriteAll("foo", []byte("bar"))
	assert.EqualError(t, err, ErrDBStoreNotInitialized.Error())

	d = NewDBStore(path.Join("/tmp/foobar-path", "db"))
	assert.Nil(t, d)

	tmppath, err := ioutil.TempDir("", "mendertest-dbstore-")
	assert.NoError(t, err)
	defer os.RemoveAll(tmppath)

	d = NewDBStore(tmppath)
	if d != nil {
		defer d.Close()
	}
	assert.NotNil(t, d)

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

		rdata, err := d.ReadAll("foo")
		assert.NoError(t, err)
		assert.Equal(t, []byte(data), rdata)
	}

	//same as above but with WriteMap
	m := map[string][]byte{}
	for i := 0; i < 2; i++ {
		key := fmt.Sprintf("map-foo-%v", i)
		value := fmt.Sprintf("map-bar-%v", i)
		m[key]=[]byte(value)
		err:=d.WriteMap(m)
		assert.NoError(t, err)
	}
	for i := 0; i < 2; i++ {
		key := fmt.Sprintf("map-foo-%v", i)
		value := fmt.Sprintf("map-bar-%v", i)
		readData, err := d.ReadAll(key)
		assert.NoError(t, err)
		assert.Equal(t, []byte(value), readData)
	}

	// try write access
	w, err := d.OpenWrite("bar")
	assert.NoError(t, err)
	w.Write([]byte("foobar"))

	// we have not committed that data yet, hence the key does not exist
	_, err = d.ReadAll("bar")
	assert.Error(t, err)

	err = w.Commit()
	assert.NoError(t, err)

	// try ReadAll()
	wdata, err := d.ReadAll("bar")
	assert.NoError(t, err)
	assert.Equal(t, wdata, []byte("foobar"))

	// once again with Reader
	r, err := d.OpenRead("bar")
	assert.NoError(t, err)
	rdata, err := ioutil.ReadAll(r)
	assert.NoError(t, err)
	assert.Equal(t, rdata, wdata)

	err = r.Close()
	assert.NoError(t, err)

	// remove the entry now
	err = d.Remove("bar")
	assert.NoError(t, err)

	// since it's removed, reading should fail
	_, err = d.ReadAll("bar")
	assert.Error(t, err)

	// also true for the reader
	_, err = d.OpenRead("bar")
	assert.Error(t, err)

	// removing once again should succeed as well
	err = d.Remove("bar")
	assert.NoError(t, err)
}
