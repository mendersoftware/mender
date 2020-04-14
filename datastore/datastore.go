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
package datastore

import (
	"encoding/json"
	"os"

	"github.com/mendersoftware/mender/store"
	"github.com/pkg/errors"
)

const (
	errMsgReadingFromStoreF = "Error reading %q from datastore."

	// This number 30 should be kept quite a lot higher than the number of
	// expected state storage operations, which is usually roughly
	// equivalent to the number of state transitions. 40 is added as an
	// extra buffer for StatusReportRetry states, which can run up to 10
	// times each (10 * two states * enter and exit state = 10 * 2 * 2 = 40)
	MaximumStateDataStoreCount = 30 + 40
)

var (
	// Special kind of error: When this error is returned by LoadStateData,
	// the StateData will also be valid, and can be used to handle the error.
	MaximumStateDataStoreCountExceeded error = errors.New(
		"State data stored and retrieved maximum number of times")
)

// Loads artifact-provides (including artifact name) needed for dependency
// checking before proceeding with installation of an artifact (version >= 3).
func LoadProvides(store store.Store) (map[string]string, error) {
	var providesBuf []byte
	var provides = make(map[string]string)
	var err error

	providesBuf, err = store.ReadAll(ArtifactNameKey)
	if err != nil && !os.IsNotExist(err) {
		return nil, errors.Wrapf(err, errMsgReadingFromStoreF,
			"ArtifactName")
	} else if err == nil {
		provides["artifact_name"] = string(providesBuf)
	}
	providesBuf, err = store.ReadAll(ArtifactGroupKey)
	if err != nil && !os.IsNotExist(err) {
		return nil, errors.Wrapf(err, errMsgReadingFromStoreF,
			"ArtifactGroup")
	} else if err == nil {
		provides["artifact_group"] = string(providesBuf)
	}
	providesBuf, err = store.ReadAll(
		ArtifactTypeInfoProvidesKey)
	if err != nil && !os.IsNotExist(err) {
		return nil, errors.Wrapf(err, errMsgReadingFromStoreF,
			"ArtifactTypeInfoProvides")
	} else if err == nil {
		if err = json.Unmarshal(providesBuf, &provides); err != nil {
			return nil, err
		}
	}
	return provides, nil
}

func StoreStateData(dbStore store.Store, sd StateData) error {
	return StoreStateDataAndTransaction(dbStore, sd, nil)
}

// Execute storing the state and a custom transaction function atomically.
func StoreStateDataAndTransaction(dbStore store.Store, sd StateData,
	txnFunc func(txn store.Transaction) error) error {

	// if the verions is not filled in, use the current one
	if sd.Version == 0 {
		sd.Version = StateDataVersion
	}

	var storeCountExceeded bool

	err := dbStore.WriteTransaction(func(txn store.Transaction) error {
		// Perform custom transaction operations, if any.
		if txnFunc != nil {
			err := txnFunc(txn)
			if err != nil {
				return err
			}
		}

		var key string
		if sd.UpdateInfo.HasDBSchemaUpdate {
			key = StateDataKeyUncommitted
		} else {
			key = StateDataKey
		}

		// See if there is an existing entry and update the store count.
		existingData, err := txn.ReadAll(key)
		if err == nil {
			var existing StateData
			err := json.Unmarshal(existingData, &existing)
			if err == nil {
				sd.UpdateInfo.StateDataStoreCount = existing.UpdateInfo.StateDataStoreCount
			}
		}

		if sd.UpdateInfo.StateDataStoreCount >= MaximumStateDataStoreCount {
			// Reset store count to prevent subsequent states from
			// hitting the same error.
			sd.UpdateInfo.StateDataStoreCount = 0
			storeCountExceeded = true
		}

		sd.UpdateInfo.StateDataStoreCount++
		data, err := json.Marshal(sd)
		if err != nil {
			return err
		}

		if !sd.UpdateInfo.HasDBSchemaUpdate {
			err = txn.Remove(StateDataKeyUncommitted)
			if err != nil {
				return err
			}
		}
		return txn.WriteAll(key, data)
	})

	if storeCountExceeded {
		return MaximumStateDataStoreCountExceeded
	}

	return err
}

func loadStateData(txn store.Transaction, key string) (StateData, error) {
	var sd StateData

	data, err := txn.ReadAll(key)
	if err != nil {
		return sd, err
	}

	// We are relying on the fact that Unmarshal will decode all and only
	// the fields that it can find in the destination type.
	err = json.Unmarshal(data, &sd)
	if err != nil {
		return sd, err
	}

	return sd, nil
}

func LoadStateData(dbStore store.Store) (StateData, error) {
	var sd StateData
	var storeCountExceeded bool

	// We do the state data loading in a write transaction so that we can
	// update the StateDataStoreCount.
	err := dbStore.WriteTransaction(func(txn store.Transaction) error {
		var err error
		sd, err = loadStateData(txn, StateDataKey)
		if err != nil {
			return err
		}

		switch sd.Version {
		case 0, 1:
			// We need to upgrade the schema. Check if we have
			// already written an updated one.
			uncommSd, err := loadStateData(txn, StateDataKeyUncommitted)
			if err == nil {
				// Verify that the update IDs are equal,
				// otherwise this may be a leftover remnant from
				// an earlier update.
				if sd.UpdateInfo.ID == uncommSd.UpdateInfo.ID {
					// Use the uncommitted one instead.
					sd = uncommSd
				}
			} else if err != os.ErrNotExist {
				return err
			}

			// If we are upgrading the schema, we know for a fact
			// that we came from a rootfs-image update, because it
			// was the only thing that was supported there. Store
			// this, since this information will be missing in
			// databases before version 2.
			sd.UpdateInfo.Artifact.PayloadTypes = []string{"rootfs-image"}
			sd.UpdateInfo.RebootRequested = []RebootType{RebootTypeCustom}
			sd.UpdateInfo.SupportsRollback = RollbackSupported

			sd.UpdateInfo.HasDBSchemaUpdate = true

		case 2:
			sd.UpdateInfo.HasDBSchemaUpdate = false

		default:
			return errors.New("unsupported state data version")
		}

		sd.Version = StateDataVersion

		if sd.UpdateInfo.StateDataStoreCount >= MaximumStateDataStoreCount {
			// Reset store count to prevent subsequent states from
			// hitting the same error.
			sd.UpdateInfo.StateDataStoreCount = 0
			storeCountExceeded = true
		}

		sd.UpdateInfo.StateDataStoreCount++
		data, err := json.Marshal(sd)
		if err != nil {
			return err
		}

		// Store the updated count back in the database.
		if sd.UpdateInfo.HasDBSchemaUpdate {
			return txn.WriteAll(StateDataKeyUncommitted, data)
		}
		return txn.WriteAll(StateDataKey, data)
	})

	if storeCountExceeded {
		return sd, MaximumStateDataStoreCountExceeded
	}

	return sd, err
}
