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
package main

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"strings"
	"syscall"

	"github.com/mendersoftware/log"
	"github.com/mendersoftware/mender/client"
	"github.com/mendersoftware/mender/installer"
	"github.com/mendersoftware/mender/store"
	"github.com/mendersoftware/mender/system"

	"github.com/pkg/errors"
)

type logOptionsType struct {
	debug      *bool
	info       *bool
	logLevel   *string
	logModules *string
	logFile    *string
	noSyslog   *bool
}

type runOptionsType struct {
	version         *bool
	config          *string
	fallbackConfig  *string
	dataStore       *string
	imageFile       *string
	commit          *bool
	rollback        *bool
	bootstrap       *bool
	daemon          *bool
	bootstrapForce  *bool
	showArtifact    *bool
	updateCheck     *bool
	updateInventory *bool
	client.Config
}

var (
	actionArguments = "-install, -commit, -rollback, -daemon, -bootstrap, -version -check-update," +
		"-send-inventory or -show-artifact"

	errMsgNoArgumentsGiven        = errors.Errorf("Must give one of %s arguments", actionArguments)
	errMsgAmbiguousArgumentsGiven = errors.Errorf("Ambiguous parameters given "+
		"- must give exactly one from: %s", actionArguments)
	errMsgIncompatibleLogOptions = errors.New("One or more " +
		"incompatible log options specified.")
)

var DeploymentLogger *DeploymentLogManager

func argsParse(args []string) (runOptionsType, error) {
	parsing := flag.NewFlagSet("mender", flag.ContinueOnError)

	// FLAGS ---------------------------------------------------------------

	version := parsing.Bool("version", false, "Show mender agent version and exit.")

	config := parsing.String("config", defaultConfFile,
		"Configuration file location.")

	fallbackConfig := parsing.String("fallback-config", defaultFallbackConfFile,
		"Fallback configuration file location.")

	data := parsing.String("data", defaultDataStore,
		"Mender state data location.")

	imageFile := parsing.String("install", "",
		"Mender Artifact to install. Can be either a local file or a URL.")

	commit := parsing.Bool("commit", false,
		"Commit current Artifact. Returns (2) if no update in progress")

	rollback := parsing.Bool("rollback", false,
		"Rollback current Artifact. Returns (2) if no update in progress")

	bootstrap := parsing.Bool("bootstrap", false, "Perform bootstrap and exit.")

	showArtifact := parsing.Bool("show-artifact", false, "print the current artifact name to the command line and exit")

	daemon := parsing.Bool("daemon", false, "Run as a daemon.")

	updateCheck := parsing.Bool("check-update", false, "force update check")

	updateInventory := parsing.Bool("send-inventory", false, "force inventory update")

	// add bootstrap related command line options
	serverCert := parsing.String("trusted-certs", "", "Trusted server certificates")
	forcebootstrap := parsing.Bool("forcebootstrap", false, "Force bootstrap")
	skipVerify := parsing.Bool("skipverify", false, "Skip certificate verification")

	// add log related command line options
	logFlags := addLogFlags(parsing)

	// PARSING -------------------------------------------------------------

	if err := parsing.Parse(args); err != nil {
		return runOptionsType{}, err
	}

	runOptions := runOptionsType{
		version:         version,
		config:          config,
		fallbackConfig:  fallbackConfig,
		dataStore:       data,
		imageFile:       imageFile,
		commit:          commit,
		rollback:        rollback,
		bootstrap:       bootstrap,
		daemon:          daemon,
		bootstrapForce:  forcebootstrap,
		showArtifact:    showArtifact,
		updateCheck:     updateCheck,
		updateInventory: updateInventory,
		Config: client.Config{
			ServerCert: *serverCert,
			NoVerify:   *skipVerify,
		},
	}

	// set info as a default log level
	log.SetLevel(log.InfoLevel)

	//runOptions.bootstrap = httpsClientConfig{}

	// FLAG LOGIC ----------------------------------------------------------

	if moreThanOneActionSelected(runOptions) {
		return runOptions, errMsgAmbiguousArgumentsGiven
	}

	if *version || *showArtifact {
		// Limit informational output for pure information queries, to
		// make it easier to use in scripts. This can still be
		// overridden by dedicated log arguments.
		log.SetLevel(log.ErrorLevel)
	}

	// we just want to see the version string check for an update,
	// or update inventory, the rest does not
	// matter.
	if *version || *updateCheck || *updateInventory {
		return runOptions, nil
	}

	if err := parseLogFlags(logFlags); err != nil {
		return runOptions, err
	}

	return runOptions, nil
}

func moreThanOneActionSelected(runOptions runOptionsType) bool {
	// check if more than one command line action is selected
	var runOptionsCount int

	if *runOptions.imageFile != "" {
		runOptionsCount++
	}
	if *runOptions.commit {
		runOptionsCount++
	}
	if *runOptions.rollback {
		runOptionsCount++
	}
	if *runOptions.daemon {
		runOptionsCount++
	}
	if *runOptions.bootstrap {
		runOptionsCount++
	}
	if *runOptions.version {
		runOptionsCount++
	}
	if *runOptions.showArtifact {
		runOptionsCount++
	}
	if *runOptions.updateCheck {
		runOptionsCount++
	}
	if *runOptions.updateInventory {
		runOptionsCount++
	}

	if runOptionsCount > 1 {
		return true
	}
	return false
}

func addLogFlags(f *flag.FlagSet) logOptionsType {

	var logOptions logOptionsType

	logOptions.debug = f.Bool("debug", false, "Debug log level. This is a "+
		"shorthand for '-log-level debug'.")

	logOptions.info = f.Bool("info", false, "Info log level. This is a "+
		"shorthand for '-log-level info'.")

	logOptions.logLevel = f.String("log-level", "", "Log level, which can be "+
		"'debug', 'info', 'warning', 'error', 'fatal' or 'panic'. "+
		"Earlier log levels will also log the subsequent levels (so "+
		"'debug' will log everything). The default log level is "+
		"'info'.")

	logOptions.logModules = f.String("log-modules", "", "Filter logging by "+
		"module. This is a comma separated list of modules to log, "+
		"other modules will be omitted. To see which modules are "+
		"available, take a look at a non-filtered log and select "+
		"the modules appropriate for you.")

	logOptions.noSyslog = f.Bool("no-syslog", false, "Disable logging to "+
		"syslog. Note that debug message are never logged to syslog.")

	logOptions.logFile = f.String("log-file", "", "File to log to.")

	return logOptions

}

func parseLogFlags(args logOptionsType) error {
	var logOptCount int

	if *args.logLevel != "" {
		level, err := log.ParseLevel(*args.logLevel)
		if err != nil {
			return err
		}
		log.SetLevel(level)
		logOptCount++
	}

	if *args.info {
		log.SetLevel(log.InfoLevel)
		logOptCount++
	}

	if *args.debug {
		log.SetLevel(log.DebugLevel)
		logOptCount++
	}

	if logOptCount > 1 {
		return errMsgIncompatibleLogOptions
	}

	if *args.logFile != "" {
		fd, err := os.Create(*args.logFile)
		if err != nil {
			return err
		}
		log.SetOutput(fd)
	}

	if *args.logModules != "" {
		modules := strings.Split(*args.logModules, ",")
		log.SetModuleFilter(modules)
	}

	if !*args.noSyslog {
		if err := log.AddSyslogHook(); err != nil {
			log.Warnf("Could not connect to syslog daemon: %s. "+
				"(use -no-syslog to disable completely)",
				err.Error())
		}
	}

	return nil
}

func ShowVersion() {
	fmt.Printf("%s\nruntime: %s\n", VersionString(), runtime.Version())
}

func PrintArtifactName(device *deviceManager) error {
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

func doBootstrapAuthorize(config *menderConfig, opts *runOptionsType) error {
	mp, err := commonInit(config, opts)
	if err != nil {
		return err
	}

	// need to close DB store manually, since we're not running under a
	// daemonized version
	defer mp.store.Close()

	controller, err := NewMender(config, *mp)
	if err != nil {
		return errors.Wrap(err, "error initializing mender controller")
	}

	if *opts.bootstrapForce {
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

func commonInit(config *menderConfig, opts *runOptionsType) (*MenderPieces, error) {

	tentok := config.GetTenantToken()

	ks := getKeyStore(*opts.dataStore, defaultKeyFile)
	if ks == nil {
		return nil, errors.New("failed to setup key storage")
	}

	stat, err := os.Stat(*opts.dataStore)
	if os.IsNotExist(err) {
		// Create data directory if it does not exist.
		err = os.MkdirAll(*opts.dataStore, 0700)
		if err != nil {
			return nil, err
		}
	} else if err != nil {
		return nil, errors.Wrapf(err, "Could not stat data directory: %s", *opts.dataStore)
	} else if !stat.IsDir() {
		return nil, errors.Errorf("%s is not a directory", *opts.dataStore)
	}

	dbstore := store.NewDBStore(*opts.dataStore)
	if dbstore == nil {
		return nil, errors.New("failed to initialize DB store")
	}

	authmgr := NewAuthManager(AuthManagerConfig{
		AuthDataStore:  dbstore,
		KeyStore:       ks,
		IdentitySource: NewIdentityDataGetter(),
		TenantToken:    tentok,
	})
	if authmgr == nil {
		// close DB store explicitly
		dbstore.Close()
		return nil, errors.New("error initializing authentication manager")
	}

	mp := MenderPieces{
		store:   dbstore,
		authMgr: authmgr,
	}
	return &mp, nil
}

func initDaemon(config *menderConfig, dev installer.DualRootfsDevice, env installer.BootEnvReadWriter,
	opts *runOptionsType) (*menderDaemon, error) {

	mp, err := commonInit(config, opts)
	if err != nil {
		return nil, err
	}
	mp.dualRootfsDevice = dev

	controller, err := NewMender(config, *mp)
	if err != nil {
		mp.store.Close()
		return nil, errors.Wrap(err, "error initializing mender controller")
	}

	if *opts.bootstrapForce {
		controller.ForceBootstrap()
	}

	daemon := NewDaemon(controller, mp.store)

	// add logging hook; only daemon needs this
	log.AddHook(NewDeploymentLogHook(DeploymentLogger))

	return daemon, nil
}

func doMain(args []string) error {
	runOptions, err := argsParse(args)
	if err != nil {
		return err
	}
	// Do not run anything else if update-check or inventory-update is triggered.
	if *runOptions.updateCheck {
		return updateCheck(exec.Command("kill", "-USR1"), exec.Command("systemctl", "show", "-p", "MainPID", "mender"))
	}
	if *runOptions.updateInventory {
		return updateCheck(exec.Command("kill", "-USR2"), exec.Command("systemctl", "show", "-p", "MainPID", "mender"))
	}

	config, err := loadConfig(*runOptions.config, *runOptions.fallbackConfig)
	if err != nil {
		return err
	}

	if runOptions.Config.NoVerify {
		config.HttpsClient.SkipVerify = true
	}

	env := installer.NewEnvironment(new(system.OsCalls))
	dualRootfsDevice := installer.NewDualRootfsDevice(env, new(system.OsCalls), config.GetDeviceConfig())
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

	DeploymentLogger = NewDeploymentLogManager(*runOptions.dataStore)

	return handleCLIOptions(runOptions, env, dualRootfsDevice, config)
}

func handleCLIOptions(runOptions runOptionsType, env *installer.UBootEnv,
	dualRootfsDevice installer.DualRootfsDevice, config *menderConfig) error {

	switch {

	case *runOptions.version:
		ShowVersion()
		return nil

	case *runOptions.showArtifact,
		*runOptions.imageFile != "",
		*runOptions.commit,
		*runOptions.rollback:

		return handleArtifactOperations(runOptions, dualRootfsDevice, config)

	case *runOptions.bootstrap:
		return doBootstrapAuthorize(config, &runOptions)

	case *runOptions.daemon:
		d, err := initDaemon(config, dualRootfsDevice, env, &runOptions)
		if err != nil {
			return err
		}
		defer d.Cleanup()
		return runDaemon(d)
	default:
		return errMsgNoArgumentsGiven
	}
}

func handleArtifactOperations(runOptions runOptionsType, dualRootfsDevice installer.DualRootfsDevice,
	config *menderConfig) error {

	menderPieces, err := commonInit(config, &runOptions)
	if err != nil {
		return err
	}
	stateExec := newStateScriptExecutor(config)
	deviceManager := NewDeviceManager(dualRootfsDevice, config, menderPieces.store)

	switch {
	case *runOptions.showArtifact:
		return PrintArtifactName(deviceManager)

	case *runOptions.imageFile != "":
		vKey := config.GetVerificationKey()
		return doStandaloneInstall(deviceManager, runOptions, vKey, stateExec)

	case *runOptions.commit:
		return doStandaloneCommit(deviceManager, stateExec)

	case *runOptions.rollback:
		return doStandaloneRollback(deviceManager, stateExec)

	default:
		return errors.New("handleArtifactOperations: Should never get here")
	}
}

func getMenderDaemonPID(cmd *exec.Cmd) (string, error) {
	buf := bytes.NewBuffer(nil)
	cmd.Stdout = buf
	err := cmd.Run()
	if err != nil {
		return "", errors.New("getMenderDaemonPID: Failed to run systemctl")
	}
	if buf.Len() == 0 {
		return "", errors.New("could not find the PID of the mender daemon")
	}
	return strings.Trim(buf.String(), "MainPID=\n"), nil
}

// updateCheck sends a SIGUSR{1,2} signal to the running mender daemon.
func updateCheck(cmdKill, cmdGetPID *exec.Cmd) error {
	pid, err := getMenderDaemonPID(cmdGetPID)
	if err != nil {
		return errors.Wrap(err, "failed to force updateCheck: ")
	}
	cmdKill.Args = append(cmdKill.Args, pid)
	err = cmdKill.Run()
	if err != nil {
		return fmt.Errorf("updateCheck: Failed to send %s the mender process, pid: %s", cmdKill.Args[len(cmdKill.Args)-1], pid)
	}
	return nil

}

func runDaemon(d *menderDaemon) error {
	// Handle user forcing update check.
	go func() {
		c := make(chan os.Signal, 2)
		signal.Notify(c, syscall.SIGUSR1) // SIGUSR1 forces an update check.
		signal.Notify(c, syscall.SIGUSR2) // SIGUSR2 forces an inventory update.
		defer signal.Stop(c)

		for {
			s := <-c // Block until a signal is received.
			if s == syscall.SIGUSR1 {
				log.Debug("SIGUSR1 signal received.")
				d.forceToState <- updateCheckState
			} else if s == syscall.SIGUSR2 {
				log.Debug("SIGUSR2 signal received.")
				d.forceToState <- inventoryUpdateState
			}
			d.sctx.wakeupChan <- true
			log.Debug("Sent wake up!")
		}
	}()
	return d.Run()
}

func main() {
	if err := doMain(os.Args[1:]); err != nil {
		var returnCode int
		if err == installer.ErrorNothingToCommit {
			log.Warnln(err.Error())
			returnCode = 2
		} else {
			if err != flag.ErrHelp {
				log.Errorln(err.Error())
			}
			returnCode = 1
		}
		os.Exit(returnCode)
	}
}
