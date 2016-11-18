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
	"bytes"
	"io"

	"github.com/mendersoftware/mender/utils"
	"github.com/stretchr/testify/mock"
)

type MemStoreWriter struct {
	bytes.Buffer
	ms   *utils.MemStore
	name string
}

func (msw *MemStoreWriter) Commit() error {
	msw.ms.Commit(msw.name, msw.Bytes())
	return nil
}

func (msw *MemStoreWriter) Close() error {
	return nil
}

type MemStoreData struct {
	data []byte
}

type MockStore struct {
	mock.Mock
}

func (ms *MockStore) ReadAll(name string) ([]byte, error) {
	ret := ms.Called(name)
	rd := ret.Get(0)
	if rd == nil {
		return nil, ret.Error(1)
	}
	return ret.Get(0).([]byte), ret.Error(1)
}

func (ms *MockStore) WriteAll(name string, data []byte) error {
	ret := ms.Called(name, data)
	return ret.Error(0)

}

func (ms *MockStore) Close() error {
	ret := ms.Called()
	return ret.Error(0)
}

func (ms *MockStore) OpenWrite(name string) (utils.WriteCloserCommitter, error) {
	ret := ms.Called(name)
	wcc := ret.Get(0)
	if wcc == nil {
		return nil, ret.Error(1)
	}
	return ret.Get(0).(utils.WriteCloserCommitter), ret.Error(1)
}

func (ms *MockStore) OpenRead(name string) (io.ReadCloser, error) {
	ret := ms.Called(name)
	rc := ret.Get(0)
	if rc == nil {
		return nil, ret.Error(1)
	}
	return ret.Get(0).(io.ReadCloser), ret.Error(1)
}

func (ms *MockStore) Remove(name string) error {
	ret := ms.Called(name)
	return ret.Error(0)
}
