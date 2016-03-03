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
import "crypto/tls"
import "crypto/x509"

type authCredsType struct {
	// hostname or address to bootstrap to
	bootstrapServer *string
	// Cert+privkey that authenticates this client
	clientCert tls.Certificate
	// Trusted server certificates
	trustedCerts x509.CertPool

	certFile   *string
	certKey    *string
	serverCert *string
}

type logOptionsType struct {
	debug      *bool
	info       *bool
	logLevel   *string
	logModules *string
	logFile    *string
	noSyslog   *bool
}

type runOptionsType struct {
	imageFile string
	commit    bool
	daemon    bool
	bootstrap authCredsType
}

var errMsgNoArgumentsGiven error = errors.New("Must give either -rootfs or -commit or -bootstrap")
var errMsgIncompatibleLogOptions error = errors.New("One or more " +
	"incompatible log log options specified.")
var errMsgDaemonMixedWithOtherOptions error = errors.New("Daemon option can not " +
	"be mixed with other options.")

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

	commit := parsing.Bool("commit", false, "Commit current update.")

	imageFile := parsing.String("rootfs", "",
		"Root filesystem URI to use for update. Can be either a local "+
			"file or a URL.")

	daemon := parsing.Bool("daemon", false, "Run as a daemon.")

	bootstrap := parsing.String("bootstrap", "", "Server to bootstrap to")

	// add log related command line options
	logFlags := addLogFlags(parsing)

	// add bootstrap related command line options
	authCreds := addBootstrapFlags(parsing)
	authCreds.bootstrapServer = bootstrap

	// PARSING -------------------------------------------------------------

	if err := parsing.Parse(args); err != nil {
		return runOptions, err
	}

	// FLAG LOGIC ----------------------------------------------------------

	if err := parseLogFlags(logFlags); err != nil {
		return runOptions, err
	}

	if err := validateBootstrap(authCreds); err != nil {
		return runOptions, err
	}

	if *daemon && (*commit || *imageFile != "") {
		// Make sure that daemon switch is not passing together with
		// commit ot rootfs
		return runOptions, errMsgDaemonMixedWithOtherOptions
	}

	if *imageFile == "" && !*commit && !*daemon {
		return runOptions, errMsgNoArgumentsGiven
	}

	runOptions.imageFile = *imageFile
	runOptions.commit = *commit
	runOptions.daemon = *daemon

	return runOptions, nil
}

func addBootstrapFlags(f *flag.FlagSet) authCredsType {
	var authCreds authCredsType

	authCreds.certFile = f.String("certificate", "",
		"Client certificate")
	authCreds.certKey = f.String("cert-key", "",
		"Client certificate's private key")
	authCreds.serverCert = f.String("trusted-certs", "",
		"Trusted server certificates")

	return authCreds
}

func validateBootstrap(args authCredsType) error {

	args.trustedCerts = *x509.NewCertPool()
	if *args.serverCert != "" {
		CertPoolAppendCertsFromFile(&args.trustedCerts, *args.serverCert)
	}

	if *args.bootstrapServer != "" && len(args.trustedCerts.Subjects()) == 0 {
		log.Warnln("No server certificate is trusted," +
			" use -trusted-certs with a proper certificate")
	}

	haveCert := false
	clientCert, err := tls.LoadX509KeyPair(*args.certFile, *args.certKey)
	if err != nil {
		log.Warnln("Failed to load certificate and key from files:",
			*args.certFile, *args.certKey)
	} else {
		args.clientCert = clientCert
		haveCert = true
	}

	if *args.bootstrapServer != "" && !haveCert {
		log.Warnln("No client certificate is provided," +
			"use options -certificate and -cert-key")
	}
	return nil
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

	// run as a daemon
	if runOptions.daemon {
		if err := runAsDemon(); err != nil {
			return err
		}
	}

	if runOptions.imageFile != "" {
		if err := doRootfs(runOptions.imageFile); err != nil {
			return err
		}
	}
	if runOptions.commit {
		if err := doCommitRootfs(); err != nil {
			return err
		}
	}
	if *runOptions.bootstrap.bootstrapServer != "" {
		err := doBootstrap(*runOptions.bootstrap.bootstrapServer,
			runOptions.bootstrap.trustedCerts,
			runOptions.bootstrap.clientCert)
		return err
	}

	return nil
}

func runAsDemon() error {
	for {

	}
}

func main() {
	if err := doMain(os.Args[1:]); err != nil && err != flag.ErrHelp {
		log.Errorln(err.Error())
		os.Exit(1)
	}
}
