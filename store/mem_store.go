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
	"bytes"
	"errors"
	"io"
	"io/ioutil"
	"os"
	"sync"
)

var (
	errDisabled = errors.New("disabled")
	errReadOnly = errors.New("read only")
)

type MemStoreWriter struct {
	bytes.Buffer
	ms   *MemStore
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

// in-memory store for testing purposes
type MemStore struct {
	data     map[string]*MemStoreData
	readonly bool
	disable  bool
	closeErr error
	mutex    sync.RWMutex
}

func (ms *MemStore) OpenRead(name string) (io.ReadCloser, error) {
	if ms.disable {
		return nil, errDisabled
	}
	v, ok := ms.data[name]
	if !ok {
		return nil, os.ErrNotExist
	}

	return ioutil.NopCloser(bytes.NewBuffer(v.data)), nil
}

func (ms *MemStore) OpenWrite(name string) (WriteCloserCommitter, error) {
	if ms.disable {
		return nil, errDisabled
	}

	if ms.readonly {
		return nil, errReadOnly
	}

	ms.data[name] = &MemStoreData{}

	msw := &MemStoreWriter{
		bytes.Buffer{},
		ms,
		name,
	}
	return msw, nil
}

func (ms *MemStore) ReadAll(name string) ([]byte, error) {
	in, err := ms.OpenRead(name)
	if err != nil {
		return nil, err
	}

	return ioutil.ReadAll(in)
}

func (ms *MemStore) WriteAll(name string, data []byte) error {
	out, err := ms.OpenWrite(name)
	if err != nil {
		return err
	}

	_, err = out.Write(data)
	if err != nil {
		return err
	}
	return out.Commit()
}

func (ms *MemStore) Close() error {
	return ms.closeErr
}

func (ms *MemStore) Commit(name string, data []byte) error {
	if ms.readonly {
		return errReadOnly
	}
	d := ms.data[name]
	d.data = data
	return nil
}

func (ms *MemStore) Remove(name string) error {
	delete(ms.data, name)
	return nil
}

func (ms *MemStore) ReadOnly(ro bool) {
	ms.readonly = ro
}

func (ms *MemStore) Disable(disable bool) {
	ms.disable = disable
}

func NewMemStore() *MemStore {
	return &MemStore{
		data: make(map[string]*MemStoreData),
	}
}

func (ms *MemStore) WriteTransaction(txnFunc func(txn Transaction) error) error {
	ms.mutex.Lock()
	defer ms.mutex.Unlock()

	return txnFunc(ms)
}
func (ms *MemStore) ReadTransaction(txnFunc func(txn Transaction) error) error {
	ms.mutex.RLock()
	defer ms.mutex.RUnlock()

	return txnFunc(ms)
}
