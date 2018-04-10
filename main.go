// Copyright 2018 Northern.tech AS
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
	"flag"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/mendersoftware/log"
	"github.com/mendersoftware/mender/client"
	"github.com/mendersoftware/mender/store"

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
	dataStore       *string
	imageFile       *string
	runStateScripts *bool
	commit          *bool
	bootstrap       *bool
	daemon          *bool
	bootstrapForce  *bool
	showArtifact    *bool
	client.Config
}

var (
	errMsgNoArgumentsGiven = errors.New("Must give one of -rootfs, " +
		"-commit, -bootstrap or -daemon arguments")
	errMsgAmbiguousArgumentsGiven = errors.New("Ambiguous parameters given " +
		"- must give exactly one from: -rootfs, -commit, -bootstrap, -authorize or -daemon")
	errMsgIncompatibleLogOptions = errors.New("One or more " +
		"incompatible log log options specified.")
)

var defaultConfFile string = path.Join(getConfDirPath(), "mender.conf")

var DeploymentLogger *DeploymentLogManager

type Commander interface {
	Command(name string, arg ...string) *exec.Cmd
}

type StatCommander interface {
	Stat(string) (os.FileInfo, error)
	Commander
}

// we need real OS implementation
type osCalls struct {
}

func (osCalls) Command(name string, arg ...string) *exec.Cmd {
	return exec.Command(name, arg...)
}

func (osCalls) Stat(name string) (os.FileInfo, error) {
	return os.Stat(name)
}

func argsParse(args []string) (runOptionsType, error) {
	parsing := flag.NewFlagSet("mender", flag.ContinueOnError)

	// FLAGS ---------------------------------------------------------------

	version := parsing.Bool("version", false, "Show mender agent version and exit.")

	config := parsing.String("config", defaultConfFile,
		"Configuration file location.")

	data := parsing.String("data", defaultDataStore,
		"Mender state data location.")

	commit := parsing.Bool("commit", false,
		"Commit current update. Returns (2) if no update in progress")

	bootstrap := parsing.Bool("bootstrap", false, "Perform bootstrap and exit.")

	showArtifact := parsing.Bool("show-artifact", false, "print the current artifact name to the command line and exit")

	imageFile := parsing.String("rootfs", "",
		"Root filesystem URI to use for update. Can be either a local "+
			"file or a URL.")

	forceStateScripts := parsing.Bool("f", false, "force installation of artifacts with state-scripts")

	daemon := parsing.Bool("daemon", false, "Run as a daemon.")

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
		dataStore:       data,
		imageFile:       imageFile,
		runStateScripts: forceStateScripts,
		commit:          commit,
		bootstrap:       bootstrap,
		daemon:          daemon,
		bootstrapForce:  forcebootstrap,
		showArtifact:    showArtifact,
		Config: client.Config{
			ServerCert: *serverCert,
			NoVerify:   *skipVerify,
		},
	}

	//runOptions.bootstrap = httpsClientConfig{}

	// FLAG LOGIC ----------------------------------------------------------

	// we just want to see the version string, the rest does not
	// matter
	if *version == true {
		return runOptions, nil
	}

	if err := parseLogFlags(logFlags); err != nil {
		return runOptions, err
	}

	if moreThanOneRunOptionSelected(runOptions) {
		return runOptions, errMsgAmbiguousArgumentsGiven
	}

	return runOptions, nil
}

func moreThanOneRunOptionSelected(runOptions runOptionsType) bool {
	// check if more than one command line action is selected
	var runOptionsCount int

	if *runOptions.imageFile != "" {
		runOptionsCount++
	}
	if *runOptions.commit {
		runOptionsCount++
	}
	if *runOptions.daemon {
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
		"shorthand for '-l debug'.")

	logOptions.info = f.Bool("info", false, "Info log level. This is a "+
		"shorthand for '-l info'.")

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
	} else if logOptCount == 0 {
		// set info as a default log level
		log.SetLevel(log.InfoLevel)
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
	v := fmt.Sprintf("%s\nruntime: %s\n", VersionString(), runtime.Version())
	os.Stdout.Write([]byte(v))
}

func PrintArtifactName(fpath string) error {
	afile, err := ioutil.ReadFile(fpath)
	if err != nil {
		return fmt.Errorf("failed to read artifact name: err: %v", err)
	}
	s := bufio.NewScanner(bytes.NewBuffer(afile))
	var aname string
	acnt := 0
	for s.Scan() {
		if strings.HasPrefix(s.Text(), "artifact_name=") {
			a := strings.Split(s.Text(), "=")
			if len(a) == 2 {
				aname = a[1]
				acnt++
			} else {
				return errors.New("Wrong formatting of the artifact_info file")
			}
		}
	}
	if acnt > 1 {
		return errors.New("there seems to be two instances of artifact_name in the artifact info file (located at /etc/mender/artifact_info). Please remove the duplicate(s)")
	}
	if aname == "" {
		return errors.New("the artifact_name is empty. Please set a valid name for the artifact")
	}
	fmt.Println(aname)
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

	controller, err := NewMender(*config, *mp)
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

func initDaemon(config *menderConfig, dev *device, env BootEnvReadWriter,
	opts *runOptionsType) (*menderDaemon, error) {

	mp, err := commonInit(config, opts)
	if err != nil {
		return nil, err
	}
	mp.device = dev

	controller, err := NewMender(*config, *mp)
	if controller == nil {
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

	config, err := LoadConfig(*runOptions.config)
	if err != nil {
		return err
	}

	if runOptions.Config.NoVerify {
		config.HttpsClient.SkipVerify = true
	}

	env := NewEnvironment(new(osCalls))
	device := NewDevice(env, new(osCalls), config.GetDeviceConfig())

	DeploymentLogger = NewDeploymentLogManager(*runOptions.dataStore)

	return handleCLIOptions(runOptions, env, device, config)
}

func handleCLIOptions(runOptions runOptionsType, env *uBootEnv, device *device, config *menderConfig) error {

	switch {

	case *runOptions.version:
		ShowVersion()
		return nil
	case *runOptions.showArtifact:
		return PrintArtifactName(filepath.Join(getConfDirPath(), "artifact_info"))

	case *runOptions.imageFile != "":
		dt, err := GetDeviceType(defaultDeviceTypeFile)
		if err != nil {
			log.Errorf("Unable to verify the existing hardware. Update will continue anyways: %v : %v", defaultDeviceTypeFile, err)
		}
		vKey := config.GetVerificationKey()
		return doRootfs(device, runOptions, dt, vKey)

	case *runOptions.commit:
		return device.CommitUpdate()
	case *runOptions.bootstrap:
		return doBootstrapAuthorize(config, &runOptions)

	case *runOptions.daemon:
		d, err := initDaemon(config, device, env, &runOptions)
		if err != nil {
			return err
		}
		defer d.Cleanup()
		return d.Run()

	case *runOptions.imageFile == "" && !*runOptions.commit &&
		!*runOptions.daemon && !*runOptions.bootstrap:
		return errMsgNoArgumentsGiven
	}

	return nil
}

func main() {
	if err := doMain(os.Args[1:]); err != nil && err != flag.ErrHelp {
		var returnCode int
		if err == errorNoUpgradeMounted {
			log.Warnln(err.Error())
			returnCode = 2
		} else {
			log.Errorln(err.Error())
			returnCode = 1
		}
		os.Exit(returnCode)
	}
}
