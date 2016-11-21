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
	"io/ioutil"
	"os"
	"path"
	"strings"
	"syscall"

	"github.com/mendersoftware/log"
	"github.com/mendersoftware/mender/cmd"
	"github.com/mendersoftware/mender/utils"
	"github.com/pkg/errors"
)

const (
	inventoryToolPrefix = "mender-inventory-"
)

func NewDataRunner(scriptsDir string) DataRunner {
	return DataRunner{
		scriptsDir,
		&cmd.OsCalls{},
	}
}

type DataRunner struct {
	dir string
	cmd cmd.Commander
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

func (id *DataRunner) Get() (Data, error) {
	tools, err := listRunnable(id.dir)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to list tools for inventory data")
	}

	idec := NewDataDecoder()
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

		p := utils.KeyValParser{}
		if err := p.Parse(out); err != nil {
			log.Warnf("inventory tool %s returned unparsable output: %v", t, err)
			continue
		}

		if err := cmd.Wait(); err != nil {
			log.Warnf("inventory tool %s wait failed: %v", t, err)
		}

		idec.appendFromRaw(p.Collect())
	}
	return idec.GetData(), nil
}

type DataDecoder struct {
	data map[string]Attribute
}

func NewDataDecoder() *DataDecoder {
	return &DataDecoder{
		make(map[string]Attribute),
	}
}

func (id *DataDecoder) GetData() Data {
	if len(id.data) == 0 {
		return nil
	}
	idata := make(Data, 0, len(id.data))
	for _, v := range id.data {
		idata = append(idata, v)
	}
	return idata
}

func (id *DataDecoder) appendFromRaw(raw map[string][]string) {
	for k, v := range raw {
		if data, ok := id.data[k]; ok {
			var newVal []string
			switch data.Value.(type) {
			case string:
				newVal = []string{data.Value.(string)}
			case []string:
				newVal = data.Value.([]string)
			}
			newVal = append(newVal, v...)
			id.data[k] = Attribute{
				Name:  k,
				Value: newVal,
			}
		} else {
			if len(v) == 1 {
				id.data[k] = Attribute{
					Name:  k,
					Value: v[0],
				}
			} else {
				id.data[k] = Attribute{
					Name:  k,
					Value: v,
				}
			}
		}
	}
}
