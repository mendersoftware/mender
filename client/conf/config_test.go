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
package conf

import (
	"io/ioutil"
	"testing"

	common "github.com/mendersoftware/mender/common/conf"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDBusUpdateControlMapExpirationTimeSecondsConfig(t *testing.T) {
	noJson, err := ioutil.TempFile("", "noJson")
	require.NoError(t, err)
	noJson.WriteString("{}")

	// unset UpdateControlMapExpirationTimeSeconds , default to 2*UpdatePollIntervalSeconds
	noVariableSet := `{
                "ServerURL": "mender.io",
                "UpdatePollIntervalSeconds": 6
        }`
	tfile, err := ioutil.TempFile("", "noVarSet")
	require.NoError(t, err)
	tfile.WriteString(noVariableSet)
	config := NewMenderConfig()
	err = common.LoadConfig(tfile.Name(), noJson.Name(), config)
	require.NoError(t, err)
	assert.Equal(t, 6*2, config.GetUpdateControlMapExpirationTimeSeconds())
	assert.Equal(t, DefaultUpdateControlMapBootExpirationTimeSeconds, config.GetUpdateControlMapBootExpirationTimeSeconds())

	// set UpdateControlMapExpirationTimeSeconds
	variableSet := `{
                "ServerURL": "mender.io",
                "UpdatePollIntervalSeconds": 6,
                "UpdateControlMapExpirationTimeSeconds": 10,
                "UpdateControlMapBootExpirationTimeSeconds": 15
        }`
	tfile, err = ioutil.TempFile("", "VarSet")
	require.NoError(t, err)
	tfile.WriteString(variableSet)
	config = NewMenderConfig()
	err = common.LoadConfig(tfile.Name(), noJson.Name(), config)
	require.NoError(t, err)
	assert.Equal(t, 10, config.GetUpdateControlMapExpirationTimeSeconds())
	assert.Equal(t, 15, config.GetUpdateControlMapBootExpirationTimeSeconds())
}
