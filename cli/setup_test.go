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
package cli

import (
	"encoding/json"
	"flag"
	"io/ioutil"
	"os"
	"path"
	"strings"
	"testing"

	"github.com/mendersoftware/mender/conf"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v2"
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
	flagSet.Bool("quiet", false, "")
	flagSet.Bool("run-daemon", false, "")
	return flagSet
}

func initCLITest(t *testing.T, flagSet *flag.FlagSet) (*cli.Context,
	*conf.MenderConfigFromFile, *runOptionsType) {
	ctx := cli.NewContext(&cli.App{}, flagSet, nil)
	ctx.Set("quiet", "true")
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

	// Demo mode no Hosted Mender
	stdinW.WriteString("blueberry-pi\n") // Device type?
	stdinW.WriteString("N\n")            // Hosted Mender?
	stdinW.WriteString("Y\n")            // Demo mode?
	stdinW.WriteString("\n")             // Server IP? (default)
	err = doSetup(ctx, config, opts)
	assert.NoError(t, err)
	assert.Equal(t, demoUpdatePoll, config.UpdatePollIntervalSeconds)
	assert.Equal(t, demoInventoryPoll, config.InventoryPollIntervalSeconds)
	assert.Equal(t, demoRetryPoll, config.RetryPollIntervalSeconds)
	assert.Equal(t, getMenderDemoCertPath(), config.ServerCertificate)

	// Demo mode with Hosted Mender
	stdinW.WriteString("banana-pi\n") // Device type?
	stdinW.WriteString("Y\n")         // Hosted Mender?
	stdinW.WriteString("Y\n")         // Demo mode?
	err = doSetup(ctx, config, opts)
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
	assert.Equal(t, "", config.ServerCertificate)
}

func TestSetupFlags(t *testing.T) {
	flagSet := newFlagSet()
	ctx, config, runOptions := initCLITest(t, flagSet)
	defer os.RemoveAll(path.Dir(runOptions.setupOptions.configPath))
	opts := &runOptions.setupOptions

	ctx.Set("tenant-token", "dummy-token")
	opts.tenantToken = "dummy-token"
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

	// Hosted Mender no demo -- same parameters as above
	ctx.Set("hosted-mender", "true")
	opts.hostedMender = true
	err = doSetup(ctx, config, opts)
	assert.NoError(t, err)
	assert.Equal(t, "https://hosted.mender.io", config.Servers[0].ServerURL)

	// Verify a few key variables that we know should not be in the
	// configuration file.
	r, err := os.Open(runOptions.setupOptions.configPath)
	require.NoError(t, err)
	var genericMap map[string]interface{}
	err = json.NewDecoder(r).Decode(&genericMap)
	assert.NoError(t, err)
	assert.Contains(t, genericMap, "UpdatePollIntervalSeconds")
	assert.NotContains(t, genericMap, "StateScriptTimeoutSeconds")
	assert.NotContains(t, genericMap, "UpdateControlMapExpirationTimeSeconds")
	assert.NotContains(t, genericMap, "UpdateControlMapBootExpirationTimeSeconds")
}

func TestInstallDemoCertificateLocalTrust(t *testing.T) {
	// NOTE: the actual call to installDemoCertificateLocalTrust will
	// fail when invoking update-ca-certificates (Permission denied).
	// This test verifies only that the certificate is copied into
	// the local trust.

	tdir, err := ioutil.TempDir("", "mendertest")
	assert.NoError(t, err)
	err = os.MkdirAll(tdir, 0755)
	assert.NoError(t, err)
	defer os.RemoveAll(tdir)

	oldDefaultLocalTrustMenderDir := DefaultLocalTrustMenderDir
	DefaultLocalTrustMenderDir = tdir
	defer func() {
		DefaultLocalTrustMenderDir = oldDefaultLocalTrustMenderDir
	}()

	oldDefaultMenderDemoCertDir := DefaultMenderDemoCertDir
	DefaultMenderDemoCertDir = path.Join("..", "support")
	defer func() {
		DefaultMenderDemoCertDir = oldDefaultMenderDemoCertDir
	}()

	stdin := os.Stdin
	stdinR, stdinW, err := os.Pipe()
	assert.NoError(t, err)
	defer func() { os.Stdin = stdin }()
	os.Stdin = stdinR

	flagSet := newFlagSet()
	ctx, config, runOptions := initCLITest(t, flagSet)
	defer os.RemoveAll(path.Dir(runOptions.setupOptions.configPath))
	opts := &runOptions.setupOptions

	// Demo mode with Demo server
	stdinW.WriteString("blueberry-pi\n") // Device type?
	stdinW.WriteString("N\n")            // Hosted Mender?
	stdinW.WriteString("Y\n")            // Demo mode?
	stdinW.WriteString("\n")             // Server IP? (default)
	err = doSetup(ctx, config, opts)
	assert.NoError(t, err)

	// Verify that the demo cert was installed in the local trust
	_, err = os.Stat(DefaultLocalTrustMenderDir)
	assert.NoError(t, err)
	crtSource, err := ioutil.ReadFile(getMenderDemoCertPath())
	assert.NoError(t, err)

	crtInstall, err := ioutil.ReadDir(DefaultLocalTrustMenderDir)
	assert.Equal(t, 2, len(crtInstall))
	for _, entry := range crtInstall {
		checkCrtInstall(t, path.Join(DefaultLocalTrustMenderDir, entry.Name()), crtSource)
	}
}

func checkCrtInstall(t *testing.T, cert string, demoCertContent []byte) {
	assert.True(t, strings.HasPrefix(path.Base(cert), DefaultLocalTrustMenderPrefix))
	contentBytes, err := ioutil.ReadFile(cert)
	content := string(contentBytes)
	require.NoError(t, err)
	assert.Greater(t, len(content), 10)
	assert.Contains(t, string(demoCertContent), content)

	lines := strings.Split(content, "\n")
	assert.Contains(t, lines[0], "BEGIN CERTIFICATE")
	assert.Contains(t, lines[len(lines)-2], "END CERTIFICATE")
	assert.Equal(t, lines[len(lines)-1], "")
}
