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
	"io"
	"io/ioutil"
	"os"
	"path"

	"github.com/bmatsuo/lmdb-go/lmdb"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
)

const (
	DBStoreName = "mender-store"
)

var (
	ErrDBStoreNotInitialized = errors.New("DB store not initialized")
)

// DBStore is an opaque structure representing a database backed storage.
// Implements `Store` interface.
type DBStore struct {
	env *lmdb.Env
}

type DBStoreWrite struct {
	io.WriteCloser
	dbs  *DBStore
	name string
	data bytes.Buffer
}

// Can be set by tests to avoid expensive sync'ing.
var LmdbNoSync bool = false

// NewDBStore creates an instance of Store backed by LMDB database. DBStore uses
// a single file for DB data (named `DBStoreName`). Parameter `dirpath` is a
// directory where the file will be stored. Returns nil if initialization
// failed.
func NewDBStore(dirpath string) *DBStore {
	env, err := lmdb.NewEnv()
	if err != nil {
		log.Errorf("Failed to create DB environment: %v", err)
		return nil
	}

	var noSyncFlag uint = 0
	if LmdbNoSync {
		noSyncFlag = lmdb.NoSync
	}
	if err := env.Open(path.Join(dirpath, DBStoreName), lmdb.NoSubdir|noSyncFlag, 0600); err != nil {
		log.Errorf("Failed to open DB environment: %v", err)
		return nil
	}

	return &DBStore{
		env: env,
	}
}

func (db *DBStore) Close() error {
	if db.env != nil {
		if err := db.env.Close(); err != nil {
			return errors.Wrapf(err, "failed to close DB")
		}
		db.env = nil
	}
	return nil
}

func (db *DBStore) ReadAll(name string) ([]byte, error) {
	if db.env == nil {
		return nil, ErrDBStoreNotInitialized
	}

	var buf []byte
	err := db.ReadTransaction(func(txn Transaction) error {
		var err error
		buf, err = txn.ReadAll(name)
		return err
	})
	return buf, err
}

func (db *DBStore) WriteAll(name string, data []byte) error {
	if db.env == nil {
		return ErrDBStoreNotInitialized
	}

	return db.WriteTransaction(func(txn Transaction) error {
		return txn.WriteAll(name, data)
	})
}

func (db *DBStore) Remove(name string) error {
	if db.env == nil {
		return ErrDBStoreNotInitialized
	}

	return db.WriteTransaction(func(txn Transaction) error {
		return txn.Remove(name)
	})
}

func (db *DBStore) OpenRead(name string) (io.ReadCloser, error) {
	b, err := db.ReadAll(name)
	if err != nil {
		return nil, err
	}
	return ioutil.NopCloser(bytes.NewBuffer(b)), nil
}

func (db *DBStore) OpenWrite(name string) (WriteCloserCommitter, error) {
	dbw := DBStoreWrite{
		dbs:  db,
		name: name,
	}
	return &dbw, nil
}

func (dbw *DBStoreWrite) Write(data []byte) (int, error) {
	return dbw.data.Write(data)
}

func (dbw *DBStoreWrite) Close() error {
	// nop
	return nil
}

func (dbw *DBStoreWrite) Commit() error {
	return dbw.dbs.WriteAll(dbw.name, dbw.data.Bytes())
}

func (db *DBStore) WriteTransaction(txnFunc func(txn Transaction) error) error {
	return db.env.Update(func(lmdbTxn *lmdb.Txn) error {
		dbi, err := lmdbTxn.OpenRoot(0)
		if err != nil {
			return err
		}
		txn := &dbTransaction{
			txn: lmdbTxn,
			dbi: dbi,
		}
		return txnFunc(txn)
	})
}

func (db *DBStore) ReadTransaction(txnFunc func(txn Transaction) error) error {
	return db.env.View(func(lmdbTxn *lmdb.Txn) error {
		dbi, err := lmdbTxn.OpenRoot(0)
		if err != nil {
			return err
		}
		txn := &dbTransaction{
			txn: lmdbTxn,
			dbi: dbi,
		}
		return txnFunc(txn)
	})
}

type dbTransaction struct {
	txn *lmdb.Txn
	dbi lmdb.DBI
}

func (txn *dbTransaction) WriteAll(name string, data []byte) error {
	return txn.txn.Put(txn.dbi, []byte(name), data, 0)
}

func (txn *dbTransaction) ReadAll(name string) ([]byte, error) {
	data, err := txn.txn.Get(txn.dbi, []byte(name))

	// conform to semantics of store read operations and return
	// os.ErrNotExist if the entry was not found
	if lmdb.IsNotFound(err) {
		return nil, os.ErrNotExist
	}

	return data, err
}

func (txn *dbTransaction) Remove(name string) error {
	if err := txn.txn.Del(txn.dbi, []byte(name), nil); err != nil {
		// don't return error if the entry we are trying to remove
		// does not exits
		if lmdbErr, ok := err.(*lmdb.OpError); ok {
			if lmdbErr.Errno == lmdb.NotFound {
				return nil
			}
		}
		return err
	}
	return nil
}
