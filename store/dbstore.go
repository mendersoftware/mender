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
	"bytes"
	"io"
	"io/ioutil"
	"os"
	"path"

	"github.com/bmatsuo/lmdb-go/lmdb"
	"github.com/mendersoftware/log"
	"github.com/pkg/errors"
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

// NewDBStore creates an instance of Store backed by LMDB database. DBStore uses
// a single file for DB data (named `DBStoreName`). Parameter `dirpath` is a
// directory where the file will be stored. Returns nil if initialization
// failed.
func NewDBStore(dirpath string) *DBStore {
	env, err := lmdb.NewEnv()
	if err != nil {
		log.Errorf("failed to create DB environment: %v", err)
		return nil
	}

	if err := env.Open(path.Join(dirpath, DBStoreName), lmdb.NoSubdir, 0600); err != nil {
		log.Errorf("failed to open DB environment: %v", err)
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
	b, err := db.readBytes(name)
	if err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

func (db *DBStore) WriteAll(name string, data []byte) error {
	return db.writeBytes(name, bytes.NewBuffer(data))
}

func (db *DBStore) writeBytes(name string, data *bytes.Buffer) error {
	if db.env == nil {
		return ErrDBStoreNotInitialized
	}

	err := db.env.Update(func(txn *lmdb.Txn) error {
		dbi, err := txn.OpenRoot(0)
		if err != nil {
			return err
		}

		if err := txn.Put(dbi, []byte(name), data.Bytes(), 0); err != nil {
			return err
		}

		return nil
	})

	if err != nil {
		return errors.Wrapf(err, "failed to read data for key %s", name)
	}
	return nil
}

func (db *DBStore) OpenRead(name string) (io.ReadCloser, error) {
	b, err := db.readBytes(name)
	if err != nil {
		return nil, err
	}
	return ioutil.NopCloser(b), nil
}

func (db *DBStore) readBytes(name string) (*bytes.Buffer, error) {
	if db.env == nil {
		return nil, ErrDBStoreNotInitialized
	}

	var b *bytes.Buffer

	err := db.env.View(func(txn *lmdb.Txn) error {
		dbi, err := txn.OpenRoot(0)
		if err != nil {
			return err
		}

		data, err := txn.Get(dbi, []byte(name))
		if err != nil {
			return err
		}

		b = bytes.NewBuffer(data)
		return nil
	})

	if err != nil {
		// conform to semantics of store read operations and return
		// os.ErrNotExist if the entry was not found
		if lmdb.IsNotFound(err) {
			return nil, os.ErrNotExist
		}
		return nil, errors.Wrapf(err, "failed to read data for key %s", name)
	}
	return b, nil
}

func (db *DBStore) Remove(name string) error {
	if db.env == nil {
		panic("env is nil")
	}

	err := db.env.Update(func(txn *lmdb.Txn) error {
		dbi, err := txn.OpenRoot(0)
		if err != nil {
			return err
		}

		if err := txn.Del(dbi, []byte(name), nil); err != nil {
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
	})

	if err != nil {
		return errors.Wrapf(err, "failed to delete key %s", name)
	}
	return nil
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
	return dbw.dbs.writeBytes(dbw.name, &dbw.data)
}
