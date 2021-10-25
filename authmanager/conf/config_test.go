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
	"io"
	"io/ioutil"
	"os"
	"path"
	"testing"

	common "github.com/mendersoftware/mender/common/conf"
	"github.com/stretchr/testify/assert"
)

var testMultipleServersConfig = `{
  "Servers": [
    {"ServerURL": "https://server.one/"},
    {"ServerURL": "https://server.two/"},
    {"ServerURL": "https://server.three/"}
  ]
}`

var testTooManyServerDefsConfig = `{
  "ServerURL": "mender.io",
  "ServerCertificate": "/var/lib/mender/server.crt",
  "Servers": [{"ServerURL": "mender.io"}]
}`

func TestServerURLConfig(t *testing.T) {
	configFile, _ := os.Create("mender.config")
	defer os.Remove("mender.config")

	configFile.WriteString(`{"ServerURL": "https://mender.io/"}`)

	config := NewAuthConfig()

	err := common.LoadConfig("mender.config", "does-not-exist.config", config)
	assert.NoError(t, err)
	err = config.Validate()
	assert.NoError(t, err)
	assert.Equal(t, "https://mender.io", config.Servers[0].ServerURL)

	// Not allowed to specify server(s) both as a list and string entry.
	configFile.Seek(0, io.SeekStart)
	configFile.WriteString(testTooManyServerDefsConfig)
	err = common.LoadConfig("mender.config", "does-not-exist.config", config)
	assert.NoError(t, err)
	err = config.Validate()
	assert.Error(t, err)
}

// TestMultipleServersConfig attempts to add multiple servers to config-
// file, as well as overriding the ServerURL from the first server.
func TestMultipleServersConfig(t *testing.T) {

	// create a temporary mender.conf file
	tdir, _ := ioutil.TempDir("", "mendertest")
	confPath := path.Join(tdir, "mender.conf")
	confFile, err := os.Create(confPath)
	defer os.RemoveAll(tdir)
	assert.NoError(t, err)

	confFile.WriteString(testMultipleServersConfig)
	// load config and assert expected values i.e. check that all entries
	// are present and URL's trailing forward slash is trimmed off.
	conf := NewAuthConfig()
	err = common.LoadConfig(confPath, "does-not-exist.config", conf)
	assert.NoError(t, err)
	conf.Validate()
	assert.NoError(t, err)
	assert.Equal(t, "https://server.one", conf.Servers[0].ServerURL)
	assert.Equal(t, "https://server.two", conf.Servers[1].ServerURL)
	assert.Equal(t, "https://server.three", conf.Servers[2].ServerURL)
}
