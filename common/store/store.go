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
	"io"

	"github.com/pkg/errors"
)

var NoTransactionSupport error = errors.New("No transaction support in this Store")

// wrapper for io.WriteCloser with extra Commit() method
type WriteCloserCommitter interface {
	io.WriteCloser
	// commit written data to data store
	Commit() error
}

type Transaction interface {
	// read in contents of entry 'name'
	ReadAll(name string) ([]byte, error)
	// write all of data to entry 'name'
	WriteAll(name string, data []byte) error
	// remove an entry
	Remove(name string) error
}

// Store is a wrapper for data store exposing a common set of methods. Errors
// returned by Store methods should preserve semantics of os I/O errors, for
// instance, OpenRead() on an entry that does not exist shall return
// os.ErrNotExist
type Store interface {
	// Works as a transaction interface as well, which auto-creates a
	// transaction for each operation.
	Transaction

	// open entry 'name' for reading
	OpenRead(name string) (io.ReadCloser, error)
	// open entry 'name' for writing, this may create a temporary entry for
	// writing data, once finished, one should call Commit() from
	// WriteCloserCommitter interface.
	OpenWrite(name string) (WriteCloserCommitter, error)

	// close the store
	Close() error

	// Runs the txnFunc function as one transaction by passing the
	// Transaction object to it. Stores that don't support transactions may
	// return NoTransactionSupport. Other errors may also be returned by the
	// txnFunc function, which will ultimately be returned by the
	// Transaction function. If an error is returned by txnFunc, the
	// transaction will be aborted instead of committed.
	WriteTransaction(txnFunc func(txn Transaction) error) error
	// Same as above, for read transactions.
	ReadTransaction(txnFunc func(txn Transaction) error) error
}
