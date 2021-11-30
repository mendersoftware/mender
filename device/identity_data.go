// Copyright 2021 Northern.tech AS
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
package device

import (
	"encoding/json"
	"path"

	"github.com/pkg/errors"

	"github.com/mendersoftware/mender/conf"
	"github.com/mendersoftware/mender/system"
	"github.com/mendersoftware/mender/utils"
)

var (
	IdentityDataHelper = path.Join(
		conf.GetDataDirPath(),
		"identity",
		"mender-device-identity",
	)
)

type IdentityDataGetter interface {
	// obtain identity data as a string or return an error
	Get() (string, error)
}

type IdentityDataRunner struct {
	Helper string
	Cmdr   system.Commander
}

func NewIdentityDataGetter() IdentityDataGetter {
	return &IdentityDataRunner{
		IdentityDataHelper,
		&system.OsCalls{},
	}
}

// Obtain identity data by calling a suitable helper tool
func (id IdentityDataRunner) Get() (string, error) {
	helper := IdentityDataHelper

	if id.Helper != "" {
		helper = id.Helper
	}

	cmd := id.Cmdr.Command(helper)

	out, err := cmd.StdoutPipe()
	if err != nil {
		return "", errors.Wrapf(err, "failed to open pipe for reading")
	}

	if err := cmd.Start(); err != nil {
		return "", errors.Wrapf(err, "failed to call %s", helper)
	}

	p := utils.KeyValParser{}
	if err := p.Parse(out); err != nil {
		return "", errors.Wrapf(err, "failed to parse identity data")
	}

	if err := cmd.Wait(); err != nil {
		return "", errors.Wrapf(err, "wait for helper failed")
	}

	collected := p.Collect()
	if len(collected) == 0 {
		return "", errors.New("no identity data colleted")
	}
	data := IdentityData{}
	data.AppendFromRaw(collected)

	encdata, err := json.Marshal(data)
	if err != nil {
		return "", errors.Wrapf(err, "failed to encode identity data")
	}

	return string(encdata), nil
}

// Try to keep things simple and reuse InventoryData as identity data structure
type IdentityData map[string]interface{}

func (id IdentityData) AppendFromRaw(raw map[string][]string) {
	for k, v := range raw {
		if len(v) == 1 {
			id[k] = v[0]
		} else {
			id[k] = v
		}
	}
}
