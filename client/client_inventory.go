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
package client

import (
	"bytes"
	"encoding/json"
	"net/http"
	"os"

	"github.com/mendersoftware/log"
	"github.com/mendersoftware/mender/store"
	"github.com/pkg/errors"
)

// DBPath is the path to the database holding inventory data
const DBPath = "/data/mender"

type InventorySubmitter interface {
	Submit(api ApiRequester, server string, data interface{}) error
}

type InventoryClient struct {
	DBPath string
	DBptr  *store.DBStore
}

func (ic *InventoryClient) GetDB() *store.DBStore {
	return ic.DBptr
}

func NewInventory() *InventoryClient {
	return &InventoryClient{DBPath, store.NewDBStore(DBPath)}
}

// ICDiffInventory returns a key-value-store with the values from n(ew)Inv that are different
// from the oldDB values. Does not handle negative diffs
func (ic *InventoryClient) ICDiffInventory(nInv []InventoryAttribute) ([]InventoryAttribute, error) {

	dInv := make([]InventoryAttribute, 0)
	for _, nv := range nInv {
		dbov, err := ic.DBptr.ReadAll(nv.Name)
		nkey := nv.Name
		nval, ok := nv.Value.(string)
		if !ok {
			return dInv, errors.New("db value is not a string")
		}
		switch err {
		case os.ErrNotExist: // db returns ErrNotExist if nkey not found
			if err := ic.DBptr.WriteAll(nkey, []byte(nval)); err != nil {
				return nil, err
			}
			dInv = append(dInv, nv)
		case nil:
			if nval != string(dbov) { // key exists, but is different
				if err := ic.DBptr.WriteAll(nkey, []byte(nval)); err != nil {
					return nil, err
				}
				dInv = append(dInv, nv)
			}
		default:
			log.Error("failed reading from database")
		}
	}

	return dInv, nil

}

// Submit reports status information to the backend
func (i *InventoryClient) Submit(api ApiRequester, url string, data interface{}) error {
	req, err := makeInventorySubmitRequest(url, data)
	if err != nil {
		return errors.Wrapf(err, "failed to prepare inventory submit request")
	}

	r, err := api.Do(req)
	if err != nil {
		log.Error("failed to submit inventory data: ", err)
		return errors.Wrapf(err, "inventory submit failed")
	}

	defer r.Body.Close()

	if r.StatusCode != http.StatusOK {
		log.Errorf("got unexpected HTTP status when submitting to inventory: %v", r.StatusCode)
		return errors.Errorf("inventory submit failed, bad status %v", r.StatusCode)
	}
	log.Debugf("inventory update sent, response %v", r)

	return nil
}

func makeInventorySubmitRequest(server string, data interface{}) (*http.Request, error) {
	url := buildApiURL(server, "/inventory/device/attributes")

	out := &bytes.Buffer{}
	enc := json.NewEncoder(out)
	enc.Encode(&data)

	hreq, err := http.NewRequest(http.MethodPatch, url, out)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to create inventory HTTP request")
	}

	hreq.Header.Add("Content-Type", "application/json")
	return hreq, nil
}
