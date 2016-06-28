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
	"encoding/json"
	"strings"

	"github.com/mendersoftware/log"
	"github.com/pkg/errors"
)

var (
	identityDataHelper = "/usr/bin/mender-device-identity"
)

type IdentityDataGetter interface {
	// obtain identity data as a string or return an error
	Get() (string, error)
}

type IdentityDataRunner struct {
	Helper string
	cmdr   Commander
}

func NewIdentityDataGetter() IdentityDataGetter {
	return &IdentityDataRunner{
		identityDataHelper,
		&osCalls{},
	}
}

// Obtain identity data by calling a suitable helper tool
func (id IdentityDataRunner) Get() (string, error) {
	helper := identityDataHelper

	if id.Helper != "" {
		helper = id.Helper
	}

	cmd := id.cmdr.Command(helper)
	data, err := cmd.Output()

	if err != nil {
		return "", errors.Wrapf(err, "failed to call %s", helper)
	}

	idata, err := parseIdentityData(data)
	if err != nil {
		return "", errors.Wrapf(err, "failed to parse identity data")
	}

	encdata, err := json.Marshal(idata)
	if err != nil {
		return "", errors.Wrapf(err, "failed to encode identity data")
	}

	return string(encdata), nil
}

// device identity data content
type IdentityData map[string]string

func parseIdentityData(data []byte) (interface{}, error) {
	idata := make(IdentityData)

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

		if _, ok := idata[val[0]]; ok {
			log.Warningf("attribute %v already present in identity data", val[0])
		}
		idata[val[0]] = val[1]
	}

	if len(idata) == 0 {
		return nil, errors.Errorf("no data found")
	}

	return &idata, nil
}
