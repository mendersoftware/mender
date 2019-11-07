// Copyright 2019 Northern.tech AS
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
package datastore

import (
	"encoding/json"
	"os"

	"github.com/mendersoftware/mender/store"
	"github.com/pkg/errors"
)

const (
	errMsgReadingFromStoreF = "Error reading %q from datastore."
)

// Loads artifact-provides (including artifact name) needed for dependency
// checking before proceeding with installation of an artifact (version >= 3).
func LoadProvidesFromStore(
	store store.Store) (map[string]interface{}, error) {
	var providesBuf []byte
	var provides = make(map[string]interface{})
	var err error

	providesBuf, err = store.ReadAll(ArtifactNameKey)
	if err != nil && !os.IsNotExist(err) {
		return nil, errors.Wrapf(err, errMsgReadingFromStoreF,
			"ArtifactName")
	} else if err == nil {
		provides["artifact_name"] = interface{}(string(providesBuf))
	}
	providesBuf, err = store.ReadAll(ArtifactGroupKey)
	if err != nil && !os.IsNotExist(err) {
		return nil, errors.Wrapf(err, errMsgReadingFromStoreF,
			"ArtifactGroup")
	} else if err == nil {
		provides["artifact_group"] = interface{}(string(providesBuf))
	}
	providesBuf, err = store.ReadAll(
		ArtifactTypeProvidesKey)
	if err != nil && !os.IsNotExist(err) {
		return nil, errors.Wrapf(err, errMsgReadingFromStoreF,
			"ArtifactTypeProvides")
	} else if err == nil {
		if err = json.Unmarshal(providesBuf, &provides); err != nil {
			return nil, err
		}
	}
	return provides, nil
}
