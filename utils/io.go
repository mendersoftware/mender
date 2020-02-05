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

package utils

import (
	"bytes"
	"errors"
	"io"
	"io/ioutil"
	"syscall"
	"testing"

	"github.com/stretchr/testify/assert"
)

type testErrorWriter struct {
	Err     error
	Written int
}

func (te *testErrorWriter) Write(p []byte) (int, error) {
	return te.Written, te.Err
}

type WriteNopCloser struct {
	io.Writer
}

func (d *WriteNopCloser) Close() error { return nil }

func TestLimitedWriteCloser(t *testing.T) {
	lw := LimitedWriteCloser{&WriteNopCloser{ioutil.Discard}, 5}
	assert.NotNil(t, lw)

	// limit to 5 bytes
	_, err := lw.Write([]byte("abcde"))
	assert.NoError(t, err)

	// ENOSPC
	_, err = lw.Write([]byte("foo"))
	assert.EqualError(t, err, syscall.ENOSPC.Error())

	b := &bytes.Buffer{}
	wc := WriteNopCloser{b}
	lw = LimitedWriteCloser{&wc, 5}
	// try to write more than 5 bytes
	w, err := lw.Write([]byte("abcdefg"))
	assert.Equal(t, 5, w)
	assert.EqualError(t, err, syscall.ENOSPC.Error())
	assert.Equal(t, []byte("abcde"), b.Bytes())

	// successful write
	b = &bytes.Buffer{}
	wc = WriteNopCloser{b}
	lw = LimitedWriteCloser{&wc, 5}
	w, err = lw.Write([]byte("foo"))
	assert.NoError(t, err)
	assert.Equal(t, len([]byte("foo")), w)

	lw = LimitedWriteCloser{nil, 100}
	_, err = lw.Write([]byte("foo"))
	assert.Error(t, err)

	lw = LimitedWriteCloser{
		W: &WriteNopCloser{&testErrorWriter{
			Err:     errors.New("fail"),
			Written: 3,
		}},
		N: 10,
	}
	w, err = lw.Write([]byte("foo"))
	// error writer pretends to have written 3 bytes
	assert.Equal(t, 3, w)
	// this should have been extracted from remaining
	assert.Equal(t, uint64(7), lw.N)
	// and we should get an error from the error writer
	assert.EqualError(t, err, "fail")
}
