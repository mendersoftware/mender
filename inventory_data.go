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
	"bufio"
	"bytes"
	"io"
	"io/ioutil"
	"os"
	"path"
	"strings"
	"syscall"

	"github.com/mendersoftware/log"
	"github.com/pkg/errors"
)

const (
	inventoryToolPrefix = "mender-inventory-"
)

func NewInventoryDataRunner(scriptsDir string) InventoryDataRunner {
	return InventoryDataRunner{
		scriptsDir,
		&osCalls{},
	}
}

type InventoryDataRunner struct {
	dir string
	cmd Commander
}

func listRunnable(dpath string) ([]string, error) {
	finfos, err := ioutil.ReadDir(dpath)
	if err != nil {
		// don't care about any FileInfo that were read up to this point
		return nil, errors.Wrapf(err, "failed to readdir")
	}

	runnable := []string{}
	for _, finfo := range finfos {
		if !strings.HasPrefix(finfo.Name(), inventoryToolPrefix) {
			continue
		}

		runBits := os.FileMode(syscall.S_IXUSR | syscall.S_IXGRP | syscall.S_IXOTH)
		if finfo.Mode()&runBits == 0 {
			continue
		}

		runnable = append(runnable, path.Join(dpath, finfo.Name()))
	}

	return runnable, nil
}

func (id *InventoryDataRunner) AddDeviceType(device string, inv InventoryData) error {
	if inv == nil {
		inv = make(InventoryData, 0, 1)
		inv = append(inv, InventoryAttribute{Name: "device_type", Value: device})
		return nil
	}

	haveDevType := false
	for _, attr := range inv {
		if strings.Compare(attr.Name, "device_type") == 0 {
			haveDevType = true
		}
	}

	if !haveDevType {
		inv = append(inv, InventoryAttribute{Name: "device_type", Value: device})
	}
	return nil
}

func (id *InventoryDataRunner) Get() (InventoryData, error) {
	tools, err := listRunnable(id.dir)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to list tools for inventory data")
	}

	idec := NewInventoryDataDecoder()
	for _, t := range tools {
		cmd := id.cmd.Command(t)
		out, err := cmd.StdoutPipe()
		if err != nil {
			log.Errorf("failed to open stdout for inventory tool %s: %v", t, err)
			continue
		}

		if err := cmd.Start(); err != nil {
			log.Errorf("inventory tool %s failed with status: %v", t, err)
			continue
		}

		if _, err := io.Copy(idec, out); err != nil {
			log.Warnf("inventory tool %s returned unparsable output: %v", t, err)
			continue
		}

		if err := cmd.Wait(); err != nil {
			log.Warnf("inventory tool %s wait failed: %v", t, err)
		}
	}
	return idec.GetInventoryData(), nil
}

type InventoryDataDecoder struct {
	data map[string]InventoryAttribute
}

func NewInventoryDataDecoder() *InventoryDataDecoder {
	return &InventoryDataDecoder{
		make(map[string]InventoryAttribute),
	}
}

func (id *InventoryDataDecoder) GetInventoryData() InventoryData {
	if len(id.data) == 0 {
		return nil
	}
	idata := make(InventoryData, 0, len(id.data))
	for _, v := range id.data {
		idata = append(idata, v)
	}
	return idata
}

func (id *InventoryDataDecoder) Write(p []byte) (n int, err error) {
	r := bufio.NewScanner(bytes.NewBuffer(p))

	for {
		if !r.Scan() {
			if r.Err() != nil {
				return 0, r.Err()
			} else {
				return len(p), nil
			}
		}
		ia, err := readAttr(r.Text())
		if err != nil {
			return 0, err
		}

		if data, ok := id.data[ia.Name]; ok {
			switch data.Value.(type) {
			case string:
				newVal := []string{data.Value.(string), ia.Value.(string)}
				id.data[ia.Name] = InventoryAttribute{ia.Name, newVal}
			case []string:
				newVal := append(data.Value.([]string), ia.Value.(string))
				id.data[ia.Name] = InventoryAttribute{ia.Name, newVal}
			}
			continue
		} else {
			id.data[ia.Name] = ia
		}
	}
}

func readAttr(p string) (InventoryAttribute, error) {
	val := strings.SplitN(p, "=", 2)
	if len(val) < 2 {
		return InventoryAttribute{}, errors.Errorf("incorrect line '%s'", p)
	}
	return InventoryAttribute{val[0], val[1]}, nil
}
