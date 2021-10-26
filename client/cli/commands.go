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
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"os/signal"
	"path"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/mendersoftware/mender/authmanager"
	"github.com/mendersoftware/mender/client/api"
	"github.com/mendersoftware/mender/client/app"
	"github.com/mendersoftware/mender/client/conf"
	"github.com/mendersoftware/mender/client/installer"
	"github.com/mendersoftware/mender/common/dbus"
	"github.com/mendersoftware/mender/common/store"
	"github.com/mendersoftware/mender/common/system"
	"github.com/mendersoftware/mender/common/tls"
	log "github.com/sirupsen/logrus"

	"github.com/pkg/errors"
	"github.com/urfave/cli/v2"
)

type logOptionsType struct {
	logLevel string
	logFile  string
	noSyslog bool
}

type runOptionsType struct {
	config         string
	fallbackConfig string
	dataStore      string
	imageFile      string
	keyPassphrase  string
	bootstrapForce bool
	tls.Config
	logOptions     logOptionsType
	setupOptions   setupOptionsType // Options for setup subcommand
	rebootExitCode bool
}

var out io.Writer = os.Stdout

var (
	errArtifactNameEmpty = errors.New("The Artifact name is empty. Please set a valid name for the Artifact!")
)

func initDualRootfsDevice(config *conf.MenderConfig) installer.DualRootfsDevice {
	env := installer.NewEnvironment(new(system.OsCalls), config.BootUtilitiesSetActivePart,
		config.BootUtilitiesGetNextActivePart)

	dualRootfsDevice := installer.NewDualRootfsDevice(
		env, new(system.OsCalls), config.GetDeviceConfig())
	if dualRootfsDevice == nil {
		log.Info("No dual rootfs configuration present")
	} else {
		ap, err := dualRootfsDevice.GetActive()
		if err != nil {
			log.Errorf("Failed to read the current active partition: %s", err.Error())
		} else {
			log.Infof("Mender running on partition: %s", ap)
		}
	}

	return dualRootfsDevice
}

var SignalHandlerChan = make(chan os.Signal, 2)

func commonInit(config *combinedConfig, opts *runOptionsType) (*app.Mender,
	*app.MenderPieces, authmanager.AuthManager, error) {

	stat, err := os.Stat(opts.dataStore)
	if os.IsNotExist(err) {
		// Create data directory if it does not exist.
		err = os.MkdirAll(opts.dataStore, 0700)
		if err != nil {
			return nil, nil, nil, err
		}
	} else if err != nil {
		return nil, nil, nil, errors.Wrapf(err, "Could not stat data directory: %s", opts.dataStore)
	} else if !stat.IsDir() {
		return nil, nil, nil, errors.Errorf("%s is not a directory", opts.dataStore)
	}

	dbstore := store.NewDBStore(opts.dataStore)
	if dbstore == nil {
		return nil, nil, nil, errors.New("failed to initialize DB store")
	}

	authManager, err := initAuthManager(config, opts)
	if err != nil {
		return nil, nil, nil, err
	}

	mp := app.MenderPieces{
		Store: dbstore,
	}

	mp.DualRootfsDevice = initDualRootfsDevice(config.MenderConfig)

	m, err := app.NewMender(config.MenderConfig, mp)
	if err != nil {
		// close DB store explicitly
		dbstore.Close()
		return nil, nil, nil, errors.Wrap(err, "error initializing mender controller")
	}

	return m, &mp, authManager, nil
}

// For use exclusively in tests, ignored otherwise.
var testDBusAPI dbus.DBusAPI = nil

func initAuthManager(config *combinedConfig, opts *runOptionsType) (authmanager.AuthManager, error) {
	dirstore := store.NewDirStore(opts.dataStore)
	dbstore := store.NewDBStore(opts.dataStore)
	if dbstore == nil {
		return nil, errors.New("failed to initialize DB store")
	}

	return authmanager.NewAuthManager(authmanager.AuthManagerConfig{
		AuthConfig:    config.AuthConfig,
		AuthDataStore: dbstore,
		KeyDirStore:   dirstore,
		KeyPassphrase: opts.keyPassphrase,
		// Normally nil, which uses the default.
		DBusAPI: testDBusAPI,
	})
}

func doBootstrapAuthorize(config *combinedConfig, opts *runOptionsType) error {
	authManager, err := initAuthManager(config, opts)
	if err != nil {
		return err
	}

	if opts.bootstrapForce {
		authManager.ForceBootstrap()
	}

	if err = authManager.Bootstrap(); err != nil {
		return err
	}

	authManager.Start()
	defer authManager.Stop()

	client, err := api.NewApiClient(time.Duration(config.AuthTimeoutSeconds)*time.Second,
		config.GetHttpConfig())
	if err != nil {
		return err
	}
	if testDBusAPI != nil {
		client.DBusAPI = testDBusAPI
	}

	// If a few seconds pass without authorization, print a helpful
	// message. Even if the device is not accepted, it will eventually time
	// out inside AuthManager.
	authResult := make(chan error, 1)
	go func() {
		_, _, err := client.RequestNewAuthToken()
		authResult <- err
	}()
	select {
	case err = <-authResult:
		return err
	case <-time.After(5 * time.Second):
		print("Waiting for authorization from server. You may have to accept the device there.\n")
	}
	return <-authResult
}

func getMenderDaemonPID(cmd *system.Cmd) (string, error) {
	buf := bytes.NewBuffer(nil)
	cmd.Stdout = buf
	err := cmd.Run()
	if err != nil {
		return "", errors.New("getMenderDaemonPID: Failed to run systemctl")
	}
	pid := strings.Trim(buf.String(), "MainPID=\n")
	if pid == "" || pid == "0" {
		return "", errors.New("could not find the PID of the mender daemon")
	}
	return pid, nil
}

func handleArtifactOperations(ctx *cli.Context, runOptions runOptionsType,
	config *conf.MenderConfig) error {

	dbstore := store.NewDBStore(runOptions.dataStore)
	if dbstore == nil {
		return errors.New("failed to initialize DB store")
	}

	dualRootfsDevice := initDualRootfsDevice(config)

	stateExec := installer.NewStateScriptExecutor(config)
	deviceManager := installer.NewDeviceManager(dualRootfsDevice, config, dbstore)

	switch ctx.Command.Name {
	case "show-artifact":
		return PrintArtifactName(deviceManager)

	case "show-provides":
		return PrintProvides(deviceManager)

	case "install":
		vKey := config.GetVerificationKey()
		return app.DoStandaloneInstall(deviceManager, runOptions.imageFile,
			runOptions.Config, vKey, stateExec, runOptions.rebootExitCode)

	case "commit":
		return app.DoStandaloneCommit(deviceManager, stateExec)

	case "rollback":
		return app.DoStandaloneRollback(deviceManager, stateExec)

	default:
		return errors.New("handleArtifactOperations: Should never get here")
	}
}

func initDaemon(config *combinedConfig,
	opts *runOptionsType) (*app.MenderDaemon, authmanager.AuthManager, error) {

	controller, mp, authManager, err := commonInit(config, opts)
	if err != nil {
		return nil, nil, err
	}

	checkDemoCert()

	if opts.bootstrapForce {
		authManager.ForceBootstrap()
	}

	daemon, err := app.NewDaemon(config.MenderConfig, controller, mp.Store)
	if err != nil {
		return nil, nil, err
	}

	// add logging hook; only daemon needs this
	log.AddHook(app.NewDeploymentLogHook(app.DeploymentLogger))

	return daemon, authManager, nil
}

func checkDemoCert() {
	entries, err := ioutil.ReadDir(DefaultLocalTrustMenderDir)
	if err != nil {
		log.Debugf("Could not open local Mender trust store directory: %s", err.Error())
		return
	}

	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), DefaultLocalTrustMenderPrefix) {
			log.Warnf("Running with demo certificate installed in trust store. This is INSECURE! "+
				"Please remove %s if you plan to use this device in production.",
				path.Join(DefaultLocalTrustMenderDir, entry.Name()))
		}
	}
}

func PrintArtifactName(device *installer.DeviceManager) error {
	name, err := device.GetCurrentArtifactName()
	if err != nil {
		return err
	} else if name == "" {
		return errArtifactNameEmpty
	}
	fmt.Fprintln(out, name)
	return nil
}

func PrintProvides(device *installer.DeviceManager) error {
	provides, err := device.GetProvides()
	if err != nil {
		return err
	}
	keys := make([]string, 0, len(provides))
	for k := range provides {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, key := range keys {
		fmt.Fprintln(out, key+"="+provides[key])
	}
	return nil
}

func runDaemon(d *app.MenderDaemon, authManager authmanager.AuthManager) error {
	// Start the AuthManager in a different go routine.
	authManager.Start()
	defer authManager.Stop()

	// Handle user forcing update check.
	go func() {
		defer signal.Stop(SignalHandlerChan)

		for {
			s := <-SignalHandlerChan // Block until a signal is received.
			if s == syscall.SIGUSR1 {
				log.Debug("SIGUSR1 signal received.")
				d.ForceToState <- app.States.UpdateCheck
			} else if s == syscall.SIGUSR2 {
				log.Debug("SIGUSR2 signal received.")
				d.ForceToState <- app.States.InventoryUpdate
			}
			d.Sctx.WakeupChan <- true
			log.Debug("Sent wake up!")
		}
	}()
	return d.Run()
}

// sendSignalToProcess sends a SIGUSR{1,2} signal to the running mender daemon.
func sendSignalToProcess(cmdKill, cmdGetPID *system.Cmd) error {
	pid, err := getMenderDaemonPID(cmdGetPID)
	if err != nil {
		return errors.Wrap(err, "failed to force updateCheck")
	}
	cmdKill.Args = append(cmdKill.Args, pid)
	err = cmdKill.Run()
	if err != nil {
		return fmt.Errorf("updateCheck: Failed to send %s the mender process, pid: %s", cmdKill.Args[len(cmdKill.Args)-1], pid)
	}
	return nil
}
