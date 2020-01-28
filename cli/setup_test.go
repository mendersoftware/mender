// Copyright 2019 Northern.tech AS
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
package cli

import (
	"io/ioutil"
	"os"
	"path"
	"testing"

	"github.com/mendersoftware/mender/conf"

	"github.com/alfrunes/cli"
	"github.com/stretchr/testify/assert"
)

func newApp(testName string) *cli.App {
	// Creates a flagset for the setup subcommand
	app := &cli.App{
		Name: testName,
		Flags: []*cli.Flag{
			{Name: "config", Default: conf.DefaultConfFile},
			{Name: "data", Default: conf.DefaultDataStore},
			{Name: "device-type"},
			{Name: "username"},
			{Name: "password"},
			{Name: "server-url", Default: defaultServerURL},
			{Name: "server-ip", Default: defaultServerIP},
			{Name: "tenant-token"},
			{Name: "trusted-certs"},
			{Name: "inventory-poll", Type: cli.Int,
				Default: defaultInventoryPoll},
			{Name: "retry-poll", Type: cli.Int,
				Default: defaultRetryPoll},
			{Name: "update-poll", Type: cli.Int,
				Default: defaultUpdatePoll},
			{Name: "hosted-mender", Type: cli.Bool},
			{Name: "demo", Type: cli.Bool},
			{Name: "quiet", Type: cli.Bool, Default: true},
		},
	}
	return app
}

func initCLITest(t *testing.T) (*cli.Context,
	*conf.MenderConfigFromFile) {
	ctx, err := cli.NewContext(newApp(t.Name()), nil, nil)
	assert.NoError(t, err)
	ctx.Set("quiet", "true")
	tmpDir, err := ioutil.TempDir("", "tmpConf")
	assert.NoError(t, err)
	confPath := path.Join(tmpDir, "mender.conf")
	config, err := conf.LoadConfig(confPath, "")
	assert.NoError(t, err)
	sysConfig := &config.MenderConfigFromFile
	sysConfig.DeviceTypeFile = path.Join(
		tmpDir, "device_type")
	ctx.Set("config", confPath)

	return ctx, sysConfig
}

func TestSetupInteractiveMode(t *testing.T) {
	stdin := os.Stdin
	stdinR, stdinW, err := os.Pipe()
	assert.NoError(t, err)
	defer func() { os.Stdin = stdin }()
	os.Stdin = stdinR

	ctx, config := initCLITest(t)
	defer func() {
		configPath, _ := ctx.String("config")
		os.RemoveAll(path.Dir(configPath))
	}()
	// Need to set tenant token to skip username/password
	// prompt in case of Hosted Mender=Y
	ctx.Set("tenant-token", "dummy-token")
	// NOTE: we also need to set the setupOptions which cli.App otherwise
	//       handles for us.

	// Demo mode no Hosted Mender
	stdinW.WriteString("blueberry-pi\n") // Device type?
	stdinW.WriteString("N\n")            // Hosted Mender?
	stdinW.WriteString("Y\n")            // Demo mode?
	stdinW.WriteString("\n")             // Server IP? (default)
	err = doSetup(ctx, config)
	assert.NoError(t, err)
	assert.Equal(t, demoUpdatePoll, config.UpdatePollIntervalSeconds)
	assert.Equal(t, demoInventoryPoll, config.InventoryPollIntervalSeconds)
	assert.Equal(t, demoRetryPoll, config.RetryPollIntervalSeconds)
	assert.Equal(t, demoServerCertificate, config.ServerCertificate)

	// Demo mode with Hosted Mender
	stdinW.WriteString("banana-pi\n") // Device type?
	stdinW.WriteString("Y\n")         // Hosted Mender?
	stdinW.WriteString("Y\n")         // Demo mode?
	err = doSetup(ctx, config)
	assert.NoError(t, err)
	assert.Equal(t, demoUpdatePoll, config.UpdatePollIntervalSeconds)
	assert.Equal(t, demoInventoryPoll, config.InventoryPollIntervalSeconds)
	assert.Equal(t, demoRetryPoll, config.RetryPollIntervalSeconds)
	assert.Equal(t, "", config.ServerCertificate)

	// Hosted Mender no demo
	stdinW.WriteString("raspberrypi3\n") // Device type?
	stdinW.WriteString("Y\n")            // Hosted Mender?
	stdinW.WriteString("N\n")            // Demo mode?
	stdinW.WriteString("100\n")          // Update poll interval
	stdinW.WriteString("200\n")          // Inventory poll interval
	stdinW.WriteString("300\n")          // Retry poll interval
	err = doSetup(ctx, config)
	assert.NoError(t, err)
	assert.Equal(t,
		100, config.UpdatePollIntervalSeconds)
	assert.Equal(t,
		200, config.InventoryPollIntervalSeconds)
	assert.Equal(t,
		300, config.RetryPollIntervalSeconds)
	assert.Equal(t,
		config.Servers[0].ServerURL,
		"https://hosted.mender.io")
	dev, err := ioutil.ReadFile(config.DeviceTypeFile)
	assert.NoError(t, err)
	assert.Equal(t, string(dev), "device_type=raspberrypi3")
	assert.Equal(t, "dummy-token", config.TenantToken)
	assert.Equal(t, "", config.ServerCertificate)

	// No demo nor Hosted Mender
	stdinW.WriteString("beagle-pi\n")               // Device type?
	stdinW.WriteString("N\n")                       // Hosted Mender?
	stdinW.WriteString("N\n")                       // Demo mode?
	stdinW.WriteString("https://acme.mender.io/\n") // ServerURL
	stdinW.WriteString("\n")                        // Server certificate
	stdinW.WriteString("\n")                        // Update poll interval
	stdinW.WriteString("\n")                        // Inventory poll interval
	stdinW.WriteString("\n")                        // Retry poll interval
	err = doSetup(ctx, config)
	assert.NoError(t, err)
	assert.Equal(t,
		config.Servers[0].ServerURL,
		"https://acme.mender.io/")
	assert.Equal(t,
		defaultUpdatePoll, config.UpdatePollIntervalSeconds)
	assert.Equal(t,
		defaultInventoryPoll, config.InventoryPollIntervalSeconds)
	assert.Equal(t,
		defaultRetryPoll, config.RetryPollIntervalSeconds)
	dev, err = ioutil.ReadFile(config.DeviceTypeFile)
	assert.NoError(t, err)
	assert.Equal(t, string(dev), "device_type=beagle-pi")
	assert.Equal(t, "", config.ServerCertificate)
}

func TestSetupFlags(t *testing.T) {
	ctx, config := initCLITest(t)
	defer func() {
		configPath, _ := ctx.String("config")
		os.RemoveAll(path.Dir(configPath))
	}()

	ctx.Set("demo", "true")
	ctx.Set("device-type", "bagel-bone")
	ctx.Set("hosted-mender", "false")
	ctx.Set("server-ip", "1.2.3.4")
	err := doSetup(ctx, config)
	assert.NoError(t, err)
	dev, err := ioutil.ReadFile(config.DeviceTypeFile)
	assert.NoError(t, err)
	assert.Equal(t, "device_type=bagel-bone", string(dev))
	assert.Equal(t, "https://docker.mender.io", config.Servers[0].ServerURL)
	assert.Equal(t, demoUpdatePoll, config.UpdatePollIntervalSeconds)
	assert.Equal(t, demoInventoryPoll, config.InventoryPollIntervalSeconds)
	assert.Equal(t, demoRetryPoll, config.RetryPollIntervalSeconds)

	ctx.Set("tenant-token", "dummy-token")
	ctx.Set("hosted-mender", "true")
	ctx.Set("device-type", "acme-pi")

	err = doSetup(ctx, config)
	assert.NoError(t, err)
	assert.Equal(t, "dummy-token", config.TenantToken)
	dev, err = ioutil.ReadFile(config.DeviceTypeFile)
	assert.NoError(t, err)
	assert.Equal(t, "device_type=acme-pi", string(dev))
	assert.Equal(t, "https://hosted.mender.io", config.Servers[0].ServerURL)
	assert.Equal(t, demoUpdatePoll, config.UpdatePollIntervalSeconds)
	assert.Equal(t, demoInventoryPoll, config.InventoryPollIntervalSeconds)
	assert.Equal(t, demoRetryPoll, config.RetryPollIntervalSeconds)

	ctx.Set("device-type", "bgl-bn")
	ctx.Set("demo", "false")
	ctx.Set("trusted-certs", "/path/to/crt")
	ctx.Set("update-poll", "123")
	ctx.Set("inventory-poll", "456")
	ctx.Set("retry-poll", "789")
	ctx.Set("hosted-mender", "false")
	ctx.Set("server-url", "https://docker.menderine.io")
	err = doSetup(ctx, config)
	assert.NoError(t, err)
	dev, err = ioutil.ReadFile(config.DeviceTypeFile)
	assert.NoError(t, err)
	assert.Equal(t, "device_type=bgl-bn", string(dev))
	assert.Equal(t, 123, config.UpdatePollIntervalSeconds)
	assert.Equal(t, 456, config.InventoryPollIntervalSeconds)
	assert.Equal(t, 789, config.RetryPollIntervalSeconds)
	assert.Equal(t, "https://docker.menderine.io",
		config.Servers[0].ServerURL)

	// Hosted Mender no demo -- same parameters as above
	ctx.Set("hosted-mender", "true")
	err = doSetup(ctx, config)
	assert.NoError(t, err)
	assert.Equal(t, "https://hosted.mender.io", config.Servers[0].ServerURL)
}
