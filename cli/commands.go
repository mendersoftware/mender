// Copyright 2020 Northern.tech AS
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
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"

	"github.com/mendersoftware/mender/app"
	"github.com/mendersoftware/mender/client"
	"github.com/mendersoftware/mender/conf"
	dev "github.com/mendersoftware/mender/device"
	"github.com/mendersoftware/mender/installer"
	"github.com/mendersoftware/mender/store"
	log "github.com/sirupsen/logrus"

	"github.com/pkg/errors"
	"github.com/urfave/cli/v2"
)

type logOptionsType struct {
	logLevel   string
	logModules string
	logFile    string
	noSyslog   bool
}

type runOptionsType struct {
	config         string
	fallbackConfig string
	dataStore      string
	imageFile      string
	bootstrapForce bool
	client.Config
	logOptions   logOptionsType
	setupOptions setupOptionsType // Options for setup subcommand
}

func commonInit(config *conf.MenderConfig, opts *runOptionsType) (*app.MenderPieces, error) {

	tentok := config.GetTenantToken()

	stat, err := os.Stat(opts.dataStore)
	if os.IsNotExist(err) {
		// Create data directory if it does not exist.
		err = os.MkdirAll(opts.dataStore, 0700)
		if err != nil {
			return nil, err
		}
	} else if err != nil {
		return nil, errors.Wrapf(err, "Could not stat data directory: %s", opts.dataStore)
	} else if !stat.IsDir() {
		return nil, errors.Errorf("%s is not a directory", opts.dataStore)
	}

	ks := getKeyStore(opts.dataStore, conf.DefaultKeyFile)
	if ks == nil {
		return nil, errors.New("failed to setup key storage")
	}

	dbstore := store.NewDBStore(opts.dataStore)
	if dbstore == nil {
		return nil, errors.New("failed to initialize DB store")
	}

	authmgr := app.NewAuthManager(app.AuthManagerConfig{
		AuthDataStore:  dbstore,
		KeyStore:       ks,
		IdentitySource: dev.NewIdentityDataGetter(),
		TenantToken:    tentok,
	})
	if authmgr == nil {
		// close DB store explicitly
		dbstore.Close()
		return nil, errors.New("error initializing authentication manager")
	}

	mp := app.MenderPieces{
		Store:   dbstore,
		AuthMgr: authmgr,
	}
	return &mp, nil
}

func doBootstrapAuthorize(config *conf.MenderConfig, opts *runOptionsType) error {
	mp, err := commonInit(config, opts)
	if err != nil {
		return err
	}

	// need to close DB store manually, since we're not running under a
	// daemonized version
	defer mp.Store.Close()

	controller, err := app.NewMender(config, *mp)
	if err != nil {
		return errors.Wrap(err, "error initializing mender controller")
	}

	if opts.bootstrapForce {
		controller.ForceBootstrap()
	}

	if merr := controller.Bootstrap(); merr != nil {
		return merr.Cause()
	}

	if merr := controller.Authorize(); merr != nil {
		return merr.Cause()
	}

	return nil
}

func getKeyStore(datastore string, keyName string) *store.Keystore {
	dirstore := store.NewDirStore(datastore)
	return store.NewKeystore(dirstore, keyName)
}

func getMenderDaemonPID(cmd *exec.Cmd) (string, error) {
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
	dualRootfsDevice installer.DualRootfsDevice,
	config *conf.MenderConfig) error {

	menderPieces, err := commonInit(config, &runOptions)
	if err != nil {
		return err
	}
	stateExec := dev.NewStateScriptExecutor(config)
	deviceManager := dev.NewDeviceManager(dualRootfsDevice, config, menderPieces.Store)

	switch ctx.Command.Name {
	case "show-artifact":
		return PrintArtifactName(deviceManager)

	case "install":
		vKey := config.GetVerificationKey()
		return app.DoStandaloneInstall(deviceManager, runOptions.imageFile,
			runOptions.Config, vKey, stateExec)

	case "commit":
		return app.DoStandaloneCommit(deviceManager, stateExec)

	case "rollback":
		return app.DoStandaloneRollback(deviceManager, stateExec)

	default:
		return errors.New("handleArtifactOperations: Should never get here")
	}
}

func initDaemon(config *conf.MenderConfig,
	dev installer.DualRootfsDevice,
	opts *runOptionsType) (*app.MenderDaemon, error) {

	mp, err := commonInit(config, opts)
	if err != nil {
		return nil, err
	}
	mp.DualRootfsDevice = dev

	controller, err := app.NewMender(config, *mp)
	if err != nil {
		mp.Store.Close()
		return nil, errors.Wrap(err, "error initializing mender controller")
	}

	if opts.bootstrapForce {
		controller.ForceBootstrap()
	}

	daemon := app.NewDaemon(controller, mp.Store)

	// add logging hook; only daemon needs this
	log.AddHook(app.NewDeploymentLogHook(app.DeploymentLogger))

	return daemon, nil
}

func PrintArtifactName(device *dev.DeviceManager) error {
	name, err := device.GetCurrentArtifactName()
	if err != nil {
		return err
	}
	if name == "" {
		return errors.New("The Artifact name is empty. Please set a valid name for the Artifact!")
	}
	fmt.Println(name)
	return nil
}

func runDaemon(d *app.MenderDaemon) error {
	// Handle user forcing update check.
	go func() {
		c := make(chan os.Signal, 3)
		signal.Notify(c, syscall.SIGUSR1) // SIGUSR1 forces an update check.
		signal.Notify(c, syscall.SIGUSR2) // SIGUSR2 forces an inventory update.
		signal.Notify(c, syscall.SIGHUP)  // SIGHUP triggers config reload.
		defer signal.Stop(c)

		for {
			s := <-c // Block until a signal is received.
			if s == syscall.SIGUSR1 {
				log.Debug("SIGUSR1 signal received.")
				d.ForceToState <- app.States.UpdateCheck
			} else if s == syscall.SIGUSR2 {
				log.Debug("SIGUSR2 signal received.")
				d.ForceToState <- app.States.InventoryUpdate
			} else if s == syscall.SIGHUP {
				d.ReloadConfig <- true
			}
			d.Sctx.WakeupChan <- true
			log.Debug("Sent wake up!")
		}
	}()
	return d.Run()
}

// updateCheck sends a SIGUSR{1,2} signal to the running mender daemon.
func updateCheck(cmdKill, cmdGetPID *exec.Cmd) error {
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
