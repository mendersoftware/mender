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
	"os"
	"strconv"

	"github.com/pkg/errors"
)

// Numeric sequence generator preserving its state in a file. The file will be
// saved in 'store' under name 'name'.
type FileSeqnum struct {
	name  string
	store Store
}

func NewFileSeqnum(name string, store Store) *FileSeqnum {
	return &FileSeqnum{
		name:  name,
		store: store,
	}
}

// Obtain next sequence number. In case of errors (read, write, parse etc.)
// returned value is 0 and error is returned.
func (fs *FileSeqnum) Get() (uint64, error) {

	d, err := fs.store.ReadAll(fs.name)
	if err != nil && !os.IsNotExist(err) {
		return 0, errors.Wrapf(err, "seqnum data read failed")
	}

	newval := SeqnumStartVal

	if !os.IsNotExist(err) {
		v, err := strconv.ParseUint(string(d), 10, 64)
		if err != nil {
			return 0, errors.Wrapf(err, "seqnum data parse failed")
		}

		// check for overflow
		if math.MaxUint64 == v {
			newval = SeqnumStartVal
		} else {
			newval = v + 1
		}
	}

	err = fs.store.WriteAll(fs.name, []byte(strconv.FormatUint(newval, 10)))
	if err != nil {
		return 0, errors.Wrapf(err, "seqnum data write failed")
	}

	return newval, nil
}
