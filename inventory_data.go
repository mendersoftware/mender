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
	dp, err := os.Open(dpath)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to open %s", dpath)
	}

	finfos, err := dp.Readdir(0)
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

type tempInventoryAttribute []string

func (tia tempInventoryAttribute) ToInventoryAttribute(name string) InventoryAttribute {
	if len(tia) > 1 {
		return InventoryAttribute{Name: name, Value: []string(tia)}
	} else if len(tia) == 1 {
		return InventoryAttribute{Name: name, Value: tia[0]}
	}
	return InventoryAttribute{Name: name, Value: ""}
}

type tempInventoryData map[string]tempInventoryAttribute

func (tid tempInventoryData) Add(name string, values ...string) {
	_, has := tid[name]
	if has {
		tid[name] = append(tid[name], values...)
	} else {
		tid[name] = values
	}
}

func (tid tempInventoryData) Append(idata tempInventoryData) {
	for k, v := range idata {
		tid.Add(k, []string(v)...)
	}
}

func (tid tempInventoryData) ToInventoryData() InventoryData {
	data := make(InventoryData, 0, len(tid))
	for k, v := range tid {
		data = append(data, v.ToInventoryAttribute(k))
	}
	return data
}

func (id *InventoryDataRunner) Get() (InventoryData, error) {
	tools, err := listRunnable(id.dir)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to list tools for inventory data")
	}

	idata := tempInventoryData{}
	for _, t := range tools {
		data, err := id.cmd.Command(t).Output()
		if err != nil {
			log.Errorf("inventory tool %s failed with status: %v", t, err)
			continue
		}

		td, err := parseInventoryData(data)
		if err != nil {
			log.Warnf("inventory tool %s returned unparsable output: %v", t, err)
			continue
		}
		idata.Append(td)
	}
	return idata.ToInventoryData(), nil
}

func parseInventoryData(data []byte) (tempInventoryData, error) {
	td := tempInventoryData{}

	in := bufio.NewScanner(bytes.NewBuffer(data))
	for in.Scan() {
		line := in.Text()

		if len(line) == 0 {
			continue
		}

		val := strings.SplitN(line, "=", 2)

		if len(val) < 2 {
			return nil, errors.Errorf("incorrect line '%s'", line)
		}

		td.Add(val[0], val[1])
	}

	if len(td) == 0 {
		return nil, errors.Errorf("obtained no output")
	}

	return td, nil
}
