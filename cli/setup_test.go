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
	"flag"
	"fmt"
	"io/ioutil"
	"os"
	"path"
	"testing"

	"github.com/mendersoftware/mender/conf"

	"github.com/stretchr/testify/assert"
	"github.com/urfave/cli"
)

func newFlagSet() *flag.FlagSet {
	// Creates a flagset for the setup subcommand
	flagSet := flag.NewFlagSet("Flags", flag.ContinueOnError)
	flagSet.String("config", "", "")
	flagSet.String("device-type", "", "")
	flagSet.String("username", "", "")
	flagSet.String("password", "", "")
	flagSet.String("server-url", "", "")
	flagSet.String("server-ip", "", "")
	flagSet.String("server-cert", "", "")
	flagSet.String("tenant-token", "", "")
	flagSet.Int("inventory-poll", defaultInventoryPoll, "")
	flagSet.Int("retry-poll", defaultRetryPoll, "")
	flagSet.Int("update-poll", defaultUpdatePoll, "")
	flagSet.Bool("hosted-mender", false, "")
	flagSet.Bool("demo", false, "")
	flagSet.Bool("run-daemon", false, "")
	return flagSet
}

func initCLITest(t *testing.T, flagSet *flag.FlagSet) (*cli.Context,
	*conf.MenderConfigFromFile, *runOptionsType) {
	ctx := cli.NewContext(&cli.App{}, flagSet, nil)
	tmpDir, err := ioutil.TempDir("", "tmpConf")
	assert.NoError(t, err)
	confPath := path.Join(tmpDir, "mender.conf")
	config, err := conf.LoadConfig(confPath, "")
	assert.NoError(t, err)
	sysConfig := &config.MenderConfigFromFile
	sysConfig.DeviceTypeFile = path.Join(
		tmpDir, "device_type")

	runOptions := runOptionsType{
		setupOptions: setupOptionsType{
			configPath: confPath,
		},
	}

	return ctx, sysConfig, &runOptions
}

func TestSetupInteractiveMode(t *testing.T) {
	stdin := os.Stdin
	stdinR, stdinW, err := os.Pipe()
	assert.NoError(t, err)
	defer func() { os.Stdin = stdin }()
	os.Stdin = stdinR

	flagSet := newFlagSet()
	ctx, config, runOptions := initCLITest(t, flagSet)
	defer os.RemoveAll(path.Dir(runOptions.setupOptions.configPath))
	opts := &runOptions.setupOptions

	// Need to set tenant token to skip username/password
	// prompt in case of Hosted Mender=Y
	ctx.Set("tenant-token", "dummy-token")
	// NOTE: we also need to set the setupOptions which cli.App otherwise
	//       handles for us.
	opts.tenantToken = "dummy-token"

	// Demo mode no hosted mender
	stdinW.WriteString("blueberry-pi\n") // Device type?
	stdinW.WriteString("Y\n")            // Confirm device?
	stdinW.WriteString("N\n")            // Hosted Mender?
	stdinW.WriteString("Y\n")            // Demo mode?
	stdinW.WriteString("\n")             // Server IP? (default)
	err = doSetup(ctx, config, opts)
	assert.NoError(t, err)
	assert.Equal(t, demoUpdatePoll, config.UpdatePollIntervalSeconds)
	assert.Equal(t, demoInventoryPoll, config.InventoryPollIntervalSeconds)
	assert.Equal(t, demoRetryPoll, config.RetryPollIntervalSeconds)

	// Demo mode with hosted mender
	stdinW.WriteString("banana-pi\n") // Device type?
	stdinW.WriteString("Y\n")         // Confirm device?
	stdinW.WriteString("Y\n")         // Hosted Mender?
	stdinW.WriteString("Y\n")         // Demo mode?
	err = doSetup(ctx, config, opts)
	assert.NoError(t, err)
	assert.Equal(t, demoUpdatePoll, config.UpdatePollIntervalSeconds)
	assert.Equal(t, demoInventoryPoll, config.InventoryPollIntervalSeconds)
	assert.Equal(t, demoRetryPoll, config.RetryPollIntervalSeconds)

	// Hosted mender no demo
	stdinW.WriteString("raspberrypi3\n") // Device type?
	stdinW.WriteString("Y\n")            // Confirm device?
	stdinW.WriteString("Y\n")            // Hosted Mender?
	stdinW.WriteString("N\n")            // Demo mode?
	stdinW.WriteString("100\n")          // Update poll interval
	stdinW.WriteString("200\n")          // Inventory poll interval
	stdinW.WriteString("300\n")          // Retry poll interval
	err = doSetup(ctx, config, opts)
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

	// No demo nor Hosted Mender
	stdinW.WriteString("beagle-pi\n")               // Device type?
	stdinW.WriteString("Y\n")                       // Confirm device?
	stdinW.WriteString("N\n")                       // Hosted Mender?
	stdinW.WriteString("N\n")                       // Demo mode?
	stdinW.WriteString("https://acme.mender.io/\n") // ServerURL
	stdinW.WriteString("\n")                        // Server certificate
	stdinW.WriteString("\n")                        // Update poll interval
	stdinW.WriteString("\n")                        // Inventory poll interval
	stdinW.WriteString("\n")                        // Retry poll interval
	err = doSetup(ctx, config, opts)
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
}

func TestSetupFlags(t *testing.T) {
	flagSet := newFlagSet()
	ctx, config, runOptions := initCLITest(t, flagSet)
	defer os.RemoveAll(path.Dir(runOptions.setupOptions.configPath))
	opts := &runOptions.setupOptions

	ctx.Set("tenant-token", "dummy-token")
	opts.tenantToken = "dummy-token"
	fmt.Println(ctx.String("tenant-token"))
	ctx.Set("hosted-mender", "true")
	opts.hostedMender = true
	ctx.Set("device-type", "acme-pi")
	opts.deviceType = "acme-pi"
	ctx.Set("demo", "true")
	opts.demo = true
	err := doSetup(ctx, config, opts)
	assert.NoError(t, err)
	assert.Equal(t, "dummy-token", config.TenantToken)
	dev, err := ioutil.ReadFile(config.DeviceTypeFile)
	assert.NoError(t, err)
	assert.Equal(t, "device_type=acme-pi", string(dev))
	assert.Equal(t, "https://hosted.mender.io", config.Servers[0].ServerURL)
	assert.Equal(t, demoUpdatePoll, config.UpdatePollIntervalSeconds)
	assert.Equal(t, demoInventoryPoll, config.InventoryPollIntervalSeconds)
	assert.Equal(t, demoRetryPoll, config.RetryPollIntervalSeconds)

	ctx.Set("device-type", "bagel-bone")
	opts.deviceType = "bagel-bone"
	ctx.Set("hosted-mender", "false")
	opts.hostedMender = false
	ctx.Set("server-ip", "1.2.3.4")
	opts.serverIP = "1.2.3.4"
	err = doSetup(ctx, config, opts)
	assert.NoError(t, err)
	dev, err = ioutil.ReadFile(config.DeviceTypeFile)
	assert.NoError(t, err)
	assert.Equal(t, "device_type=bagel-bone", string(dev))
	assert.Equal(t, "https://docker.mender.io", config.Servers[0].ServerURL)
	assert.Equal(t, demoUpdatePoll, config.UpdatePollIntervalSeconds)
	assert.Equal(t, demoInventoryPoll, config.InventoryPollIntervalSeconds)
	assert.Equal(t, demoRetryPoll, config.RetryPollIntervalSeconds)

	ctx.Set("device-type", "bgl-bn")
	opts.deviceType = "bgl-bn"
	ctx.Set("demo", "false")
	opts.demo = false
	ctx.Set("server-cert", "/path/to/crt")
	opts.serverCert = "/path/to/crt"
	ctx.Set("update-poll", "123")
	opts.updatePollInterval = 123
	ctx.Set("inventory-poll", "456")
	opts.invPollInterval = 456
	ctx.Set("retry-poll", "789")
	opts.retryPollInterval = 789
	ctx.Set("hosted-mender", "false")
	fmt.Println(ctx.Bool("hosted-mender"))
	opts.hostedMender = false
	ctx.Set("server-url", "https://docker.menderine.io")
	opts.serverURL = "https://docker.menderine.io"
	err = doSetup(ctx, config, opts)
	assert.NoError(t, err)
	dev, err = ioutil.ReadFile(config.DeviceTypeFile)
	assert.NoError(t, err)
	assert.Equal(t, "device_type=bgl-bn", string(dev))
	assert.Equal(t, 123, config.UpdatePollIntervalSeconds)
	assert.Equal(t, 456, config.InventoryPollIntervalSeconds)
	assert.Equal(t, 789, config.RetryPollIntervalSeconds)
	assert.Equal(t, "https://docker.menderine.io",
		config.Servers[0].ServerURL)

	// Hosted mender no demo -- same parameters as above
	ctx.Set("hosted-mender", "true")
	opts.hostedMender = true
	err = doSetup(ctx, config, opts)
	assert.NoError(t, err)
	assert.Equal(t, "https://hosted.mender.io", config.Servers[0].ServerURL)
}
