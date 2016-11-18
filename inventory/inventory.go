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

package inventory

import (
	"time"

	"github.com/mendersoftware/log"
	"github.com/mendersoftware/mender/client"
	"github.com/pkg/errors"
)

type InventoryAttribute struct {
	Name  string      `json:"name"`
	Value interface{} `json:"value"`
}

type InventoryData []InventoryAttribute

func (id *InventoryData) ReplaceAttributes(attr []InventoryAttribute) error {
	iMap := make(map[string]InventoryAttribute, len(*id))
	for _, ia := range *id {
		iMap[ia.Name] = ia
	}

	for _, ia := range attr {
		iMap[ia.Name] = ia
	}

	cnt := 0
	for _, v := range iMap {
		if len(*id) > cnt {
			(*id)[cnt] = v
		} else {
			*id = append(*id, v)
		}
		cnt++
	}
	return nil
}

type Inventory struct {
	sendTicker  *time.Ticker
	freq        time.Duration
	scriptsPath string
	submitter   client.InventorySubmitter

	canSend bool
}

func New(path string, freq time.Duration, iSubmitter client.InventorySubmitter) *Inventory {
	ticker := time.NewTicker(freq)
	tickChan := ticker.C

	i := Inventory{
		sendTicker:  ticker,
		freq:        freq,
		scriptsPath: path,
		submitter:   iSubmitter,
	}

	go func() {
		for {
			select {
			case <-tickChan:
				i.canSend = true
			}
		}
	}()

	return &i
}

func (i *Inventory) Close() {
	if i.sendTicker != nil {
		i.sendTicker.Stop()
	}
}

func (i *Inventory) send(req *client.ApiRequest, uri string, extraAttr []InventoryAttribute) error {

	idg := NewDataRunner(i.scriptsPath)

	idata, err := idg.Get()
	if err != nil {
		// at least report device type
		log.Errorf("failed to obtain inventory data: %s", err.Error())
	}

	if idata == nil && extraAttr == nil {
		log.Infof("no inventory data to submit")
		return nil
	}

	if idata == nil {
		idata = make(InventoryData, 0, len(extraAttr))
	}
	idata.ReplaceAttributes(extraAttr)

	err = i.submitter.Submit(req, uri, idata)
	if err != nil {
		return errors.Wrapf(err, "failed to submit inventory data")
	}
	return nil
}

func (i *Inventory) Send(req *client.ApiRequest, uri string, extraAttr []InventoryAttribute) error {
	if i.canSend {
		err := i.send(req, uri, extraAttr)
		if err != nil {
			i.canSend = false
			return nil
		}
		return err
	}
	return nil
}

func (i *Inventory) SendNow(req *client.ApiRequest, uri string, extraAttr []InventoryAttribute) error {
	return i.send(req, uri, extraAttr)
}
