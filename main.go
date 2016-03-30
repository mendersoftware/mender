// Copyright 2016 Mender Software AS
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
	"errors"
	"flag"
	"os"
	"os/exec"
	"strings"

	"github.com/mendersoftware/log"
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
	imageFile       *string
	commit          *bool
	daemon          *bool
	bootstrapServer *string
	httpsClientConfig
}

var (
	errMsgNoArgumentsGiven = errors.New("Must give one of -rootfs, " +
		"-commit, -bootstrap or -daemon arguments")
	errMsgAmbiguousArgumentsGiven = errors.New("Ambiguous parameters given " +
		"- must give exactly one from: -rootfs, -commit, -bootstrap or -daemon")
	errMsgIncompatibleLogOptions = errors.New("One or more " +
		"incompatible log log options specified.")
)

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

	commit := parsing.Bool("commit", false, "Commit current update.")

	imageFile := parsing.String("rootfs", "",
		"Root filesystem URI to use for update. Can be either a local "+
			"file or a URL.")

	daemon := parsing.Bool("daemon", false, "Run as a daemon.")

	// add bootstrap related command line options
	certFile := parsing.String("certificate", "", "Client certificate")
	certKey := parsing.String("cert-key", "", "Client certificate's private key")
	serverCert := parsing.String("trusted-certs", "", "Trusted server certificates")
	bootstrapServer := parsing.String("bootstrap", "", "Server to bootstrap to")

	// add log related command line options
	logFlags := addLogFlags(parsing)

	// PARSING -------------------------------------------------------------

	if err := parsing.Parse(args); err != nil {
		return runOptionsType{}, err
	}

	runOptions := runOptionsType{imageFile, commit, daemon, bootstrapServer,
		httpsClientConfig{*certFile, *certKey, *serverCert, false},
	}

	//runOptions.bootstrap = httpsClientConfig{}

	// FLAG LOGIC ----------------------------------------------------------

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
	if *runOptions.bootstrapServer != "" {
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
		"'warning'.")

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
		// Default log level.
		log.SetLevel(log.WarnLevel)
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

func doMain(args []string) error {
	runOptions, err := argsParse(args)
	if err != nil {
		return err
	}

	// in any case we will need to have a device
	env := NewEnvironment(new(osCalls))
	device := NewDevice(env, new(osCalls), "/dev/mmcblk0p")

	switch {

	case *runOptions.imageFile != "":
		if err := doRootfs(device, runOptions); err != nil {
			return err
		}

	case *runOptions.commit:
		if err := device.CommitUpdate(); err != nil {
			return err
		}

	case *runOptions.daemon:
		controler := NewMender(env)
		if err := controler.LoadConfig("/etc/mender/mender.conf"); err != nil {
			return err
		}

		updater, err := NewUpdater(controler.GetUpdaterConfig())
		if err != nil {
			return errors.New("Can not initialize daemon. Error instantiating updater. Exiting.")
		}
		daemon := NewDaemon(updater, device, controler)
		return daemon.Run()

	case *runOptions.bootstrapServer != "":
		return doBootstrap(runOptions.httpsClientConfig, *runOptions.bootstrapServer)

	case *runOptions.imageFile == "" && !*runOptions.commit &&
		!*runOptions.daemon && *runOptions.bootstrapServer == "":
		return errMsgNoArgumentsGiven
	}

	return nil
}

func main() {
	if err := doMain(os.Args[1:]); err != nil && err != flag.ErrHelp {
		log.Errorln(err.Error())
		os.Exit(1)
	}
}
