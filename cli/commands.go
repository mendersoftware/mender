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

	log "github.com/sirupsen/logrus"

	"github.com/pkg/errors"
	"github.com/urfave/cli/v2"

	"github.com/mendersoftware/mender/app"
	"github.com/mendersoftware/mender/client"
	"github.com/mendersoftware/mender/conf"
	"github.com/mendersoftware/mender/dbus"
	dev "github.com/mendersoftware/mender/device"
	"github.com/mendersoftware/mender/installer"
	"github.com/mendersoftware/mender/store"
	"github.com/mendersoftware/mender/system"
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
	client.Config
	logOptions     logOptionsType
	setupOptions   setupOptionsType // Options for setup subcommand
	rebootExitCode bool
}

var out io.Writer = os.Stdout

var (
	errArtifactNameEmpty = errors.New(
		"The Artifact name is empty. Please set a valid name for the Artifact!",
	)
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

func commonInit(
	config *conf.MenderConfig,
	opts *runOptionsType,
) (*app.Mender, *app.MenderPieces, error) {

	tentok := config.GetTenantToken()

	stat, err := os.Stat(opts.dataStore)
	if os.IsNotExist(err) {
		// Create data directory if it does not exist.
		err = os.MkdirAll(opts.dataStore, 0700)
		if err != nil {
			return nil, nil, err
		}
	} else if err != nil {
		return nil, nil, errors.Wrapf(err, "Could not stat data directory: %s", opts.dataStore)
	} else if !stat.IsDir() {
		return nil, nil, errors.Errorf("%s is not a directory", opts.dataStore)
	}

	var (
		ks       *store.Keystore
		dirstore *store.DirStore
	)
	dirstore = store.NewDirStore(opts.dataStore)
	var privateKey string
	var sslEngine string
	var static bool

	if config.HttpsClient.Key != "" {
		privateKey = config.HttpsClient.Key
		sslEngine = config.HttpsClient.SSLEngine
		static = true
	}
	if config.Security.AuthPrivateKey != "" {
		privateKey = config.Security.AuthPrivateKey
		sslEngine = config.Security.SSLEngine
		static = true
	}
	if config.HttpsClient.Key == "" && config.Security.AuthPrivateKey == "" {
		privateKey = conf.DefaultKeyFile
		sslEngine = config.HttpsClient.SSLEngine
		static = false
	}

	ks = store.NewKeystore(dirstore, privateKey, sslEngine, static, opts.keyPassphrase)
	if ks == nil {
		return nil, nil, errors.New("failed to setup key storage")
	}

	dbstore := store.NewDBStore(opts.dataStore)
	if dbstore == nil {
		return nil, nil, errors.New("failed to initialize DB store")
	}

	authmgr := app.NewAuthManager(app.AuthManagerConfig{
		AuthDataStore:  dbstore,
		KeyStore:       ks,
		IdentitySource: dev.NewIdentityDataGetter(),
		TenantToken:    tentok,
		Config:         config,
	})
	if authmgr == nil {
		// close DB store explicitly
		dbstore.Close()
		return nil, nil, errors.New("error initializing authentication manager")
	}

	if config.DBus.Enabled {
		api, err := dbus.GetDBusAPI()
		if err != nil {
			// close DB store explicitly
			dbstore.Close()
			return nil, nil, errors.Wrap(err, "DBus API support not available, but DBus is enabled")
		}
		authmgr.EnableDBus(api)
	}

	mp := app.MenderPieces{
		Store:       dbstore,
		AuthManager: authmgr,
	}

	mp.DualRootfsDevice = initDualRootfsDevice(config)

	m, err := app.NewMender(config, mp)
	if err != nil {
		// close DB store explicitly
		dbstore.Close()
		return nil, nil, errors.Wrap(err, "error initializing mender controller")
	}

	return m, &mp, nil
}

func doBootstrapAuthorize(config *conf.MenderConfig, opts *runOptionsType) error {
	controller, mp, err := commonInit(config, opts)
	if err != nil {
		return err
	}

	// need to close DB store manually, since we're not running under a
	// daemonized version
	defer mp.Store.Close()

	authManager := mp.AuthManager
	if opts.bootstrapForce {
		authManager.ForceBootstrap()
	}

	if merr := authManager.Bootstrap(); merr != nil {
		return merr.Cause()
	}

	authManager.Start()
	defer authManager.Stop()

	_, _, err = controller.Authorize()

	return err
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

	stateExec := dev.NewStateScriptExecutor(config)
	deviceManager := dev.NewDeviceManager(dualRootfsDevice, config, dbstore)

	switch ctx.Command.Name {
	case "show-artifact":
		return PrintArtifactName(deviceManager)

	case "show-provides":
		return PrintProvides(deviceManager)

	case "install":
		return app.DoStandaloneInstall(deviceManager, runOptions.imageFile,
			runOptions.Config, stateExec, runOptions.rebootExitCode)

	case "commit":
		return app.DoStandaloneCommit(deviceManager, stateExec)

	case "rollback":
		return app.DoStandaloneRollback(deviceManager, stateExec)

	default:
		return errors.New("handleArtifactOperations: Should never get here")
	}
}

func initDaemon(config *conf.MenderConfig,
	opts *runOptionsType) (*app.MenderDaemon, error) {

	controller, mp, err := commonInit(config, opts)
	if err != nil {
		return nil, err
	}

	checkDemoCert()

	if opts.bootstrapForce {
		authManager := mp.AuthManager
		authManager.ForceBootstrap()
	}

	daemon, err := app.NewDaemon(config, controller, mp.Store, mp.AuthManager)
	if err != nil {
		return nil, err
	}

	// add logging hook; only daemon needs this
	log.AddHook(app.NewDeploymentLogHook(app.DeploymentLogger))

	// At the moment we don't do anything with this, just force linking to it.
	_, _ = dbus.GetDBusAPI()

	return daemon, nil
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

func PrintArtifactName(device *dev.DeviceManager) error {
	name, err := device.GetCurrentArtifactName()
	if err != nil {
		return err
	} else if name == "" {
		return errArtifactNameEmpty
	}
	fmt.Fprintln(out, name)
	return nil
}

func PrintProvides(device *dev.DeviceManager) error {
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

func runDaemon(d *app.MenderDaemon) error {
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
		return fmt.Errorf(
			"updateCheck: Failed to send %s the mender process, pid: %s",
			cmdKill.Args[len(cmdKill.Args)-1],
			pid,
		)
	}
	return nil
}
