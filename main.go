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

import "errors"
import "flag"
import "github.com/mendersoftware/log"
import "io/ioutil"
import "os"
import "strings"
import "crypto/x509"

type logOptionsType struct {
	debug      *bool
	info       *bool
	logLevel   *string
	logModules *string
	logFile    *string
	noSyslog   *bool
}

type runOptionsType struct {
	imageFile *string
	commit    *bool
	daemon    *bool
	bootstrap *authCmdLineArgsType
}

var errMsgNoArgumentsGiven error = errors.New("Must give one of -rootfs, " +
	"-commit, -bootstrap or -daemon arguments")
var errMsgAmbiguousArgumentsGiven error = errors.New("Ambiguous parameters given " +
	"- must give exactly one from: -rootfs, -commit, -bootstrap or -daemon")
var errMsgIncompatibleLogOptions error = errors.New("One or more " +
	"incompatible log log options specified.")

func CertPoolAppendCertsFromFile(s *x509.CertPool, f string) bool {
	cacert, err := ioutil.ReadFile(f)
	if err != nil {
		log.Warnln("Error reading file", f, err)
		return false
	}

	ret := s.AppendCertsFromPEM(cacert)
	return ret
}

func argsParse(args []string) (runOptionsType, error) {
	var runOptions runOptionsType

	parsing := flag.NewFlagSet("mender", flag.ContinueOnError)

	// FLAGS ---------------------------------------------------------------

	runOptions.commit = parsing.Bool("commit", false, "Commit current update.")

	runOptions.imageFile = parsing.String("rootfs", "",
		"Root filesystem URI to use for update. Can be either a local "+
			"file or a URL.")

	runOptions.daemon = parsing.Bool("daemon", false, "Run as a daemon.")

	// add bootstrap related command line options
	authCreds := addBootstrapFlags(parsing)
	authCreds.bootstrapServer = *parsing.String("bootstrap", "", "Server to bootstrap to")
	runOptions.bootstrap = authCreds

	// add log related command line options
	logFlags := addLogFlags(parsing)

	// PARSING -------------------------------------------------------------

	if err := parsing.Parse(args); err != nil {
		return runOptions, err
	}

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
	var runOptionsCount int = 0

	if *runOptions.imageFile != "" {
		runOptionsCount += 1
	}
	if *runOptions.commit {
		runOptionsCount += 1
	}
	if *runOptions.daemon {
		runOptionsCount += 1
	}
	if runOptions.bootstrap.bootstrapServer != "" {
		runOptionsCount += 1
	}

	if runOptionsCount > 1 {
		return true
	}
	return false
}

func addBootstrapFlags(f *flag.FlagSet) *authCmdLineArgsType {
	var authCreds authCmdLineArgsType

	authCreds.certFile = *f.String("certificate", "",
		"Client certificate")
	authCreds.certKey = *f.String("cert-key", "",
		"Client certificate's private key")
	authCreds.serverCert = *f.String("trusted-certs", "",
		"Trusted server certificates")

	return &authCreds
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
	var logOptCount int = 0

	if *args.logLevel != "" {
		level, err := log.ParseLevel(*args.logLevel)
		if err != nil {
			return err
		}
		log.SetLevel(level)
		logOptCount += 1
	}

	if *args.info {
		log.SetLevel(log.InfoLevel)
		logOptCount += 1
	}

	if *args.debug {
		log.SetLevel(log.DebugLevel)
		logOptCount += 1
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

	switch {

	case *runOptions.imageFile != "":
		if err := doRootfs(*runOptions.imageFile); err != nil {
			return err
		}

	case *runOptions.commit:
		if err := doCommitRootfs(); err != nil {
			return err
		}

	case *runOptions.daemon:
		// first make sure we are reusing authentication provided by bootstrap
		runOptions.bootstrap.setDefaultKeysAndCerts(defaultCertFile, defaultCertKey, defaultServerCert)
		err, authCreds := initClientAndServerAuthCreds(runOptions.bootstrap)
		if err != nil {
			return err
		}
		client := &Client{"", initClient(authCreds)}
		var config daemonConfigType
		config.setPullInterval(defaultServerPullInterval)

		if err := runAsDemon(config, client); err != nil {
			return err
		}

	case runOptions.bootstrap.bootstrapServer != "":
		// set default values if nothing is provided via command line
		runOptions.bootstrap.setDefaultKeysAndCerts(defaultCertFile, defaultCertKey, defaultServerCert)

		err, authCreds := initClientAndServerAuthCreds(runOptions.bootstrap)
		if err != nil {
			return err
		}

		client := &Client{"https://" + runOptions.bootstrap.bootstrapServer, initClient(authCreds)}
		if err, _ := client.doBootstrap(); err != nil {
			return err
		}

		//TODO: store bootstrap credentials so that we will be able to reuse in future

	case *runOptions.imageFile == "" && !*runOptions.commit &&
		!*runOptions.daemon && runOptions.bootstrap.bootstrapServer == "":
		return errMsgNoArgumentsGiven

	default:
		// have invalid argument
		return flag.ErrHelp
	}

	return nil
}

func main() {
	if err := doMain(os.Args[1:]); err != nil && err != flag.ErrHelp {
		log.Errorln(err.Error())
		os.Exit(1)
	}
}
