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

type runOptionsType struct {
	imageFile  string
	committing bool
	// Cert+privkey that authenticates this client
	clientCert tls.Certificate
	// Trusted server certificates
	trustedCerts x509.CertPool
	// hostname or address to bootstrap to
	bootstrap string
}

var errMsgNoArgumentsGiven error = errors.New("Must give either -rootfs or -commit or -bootstrap")
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

	committing := parsing.Bool("commit", false, "Commit current update.")

	debug := parsing.Bool("debug", false, "Debug log level. This is a "+
		"shorthand for '-l debug'.")

	info := parsing.Bool("info", false, "Info log level. This is a "+
		"shorthand for '-l info'.")

	imageFile := parsing.String("rootfs", "",
		"Root filesystem URI to use for update. Can be either a local "+
			"file or a URL.")

	logLevel := parsing.String("log-level", "", "Log level, which can be "+
		"'debug', 'info', 'warning', 'error', 'fatal' or 'panic'. "+
		"Earlier log levels will also log the subsequent levels (so "+
		"'debug' will log everything). The default log level is "+
		"'warning'.")

	logModules := parsing.String("log-modules", "", "Filter logging by "+
		"module. This is a comma separated list of modules to log, "+
		"other modules will be omitted. To see which modules are "+
		"available, take a look at a non-filtered log and select "+
		"the modules appropriate for you.")

	noSyslog := parsing.Bool("no-syslog", false, "Disable logging to "+
		"syslog. Note that debug message are never logged to syslog.")

	logFile := parsing.String("log-file", "", "File to log to.")

	bootstrap := parsing.String("bootstrap", "",
		"Server to bootstrap to")

	certFile := parsing.String("certificate", "",
		"Client certificate")
	certKey := parsing.String("cert-key", "",
		"Client certificate's private key")
	serverCert := parsing.String("trusted-certs", "",
		"Trusted server certificates")

	// PARSING -------------------------------------------------------------

	if err := parsing.Parse(args); err != nil {
		return runOptions, err
	}

	// FLAG LOGIC ----------------------------------------------------------

	var logOptCount int = 0

	if *logLevel != "" {
		level, err := log.ParseLevel(*logLevel)
		if err != nil {
			return runOptions, err
		}
		log.SetLevel(level)
		logOptCount += 1
	}

	if *info {
		log.SetLevel(log.InfoLevel)
		logOptCount += 1
	}

	if *debug {
		log.SetLevel(log.DebugLevel)
		logOptCount += 1
	}

	if logOptCount > 1 {
		return runOptions, errMsgIncompatibleLogOptions
	} else if logOptCount == 0 {
		// Default log level.
		log.SetLevel(log.WarnLevel)
	}

	if *logFile != "" {
		fd, err := os.Create(*logFile)
		if err != nil {
			return runOptions, err
		}
		log.SetOutput(fd)
	}

	if *logModules != "" {
		modules := strings.Split(*logModules, ",")
		log.SetModuleFilter(modules)
	}

	if !*noSyslog {
		if err := log.AddSyslogHook(); err != nil {
			log.Warnf("Could not connect to syslog daemon: %s. "+
				"(use -no-syslog to disable completely)",
				err.Error())
		}
	}

	if *imageFile == "" && !*committing && *bootstrap == "" {
		return runOptions, errMsgNoArgumentsGiven
	}

	runOptions.imageFile = *imageFile
	runOptions.committing = *committing
	runOptions.bootstrap = *bootstrap

	runOptions.trustedCerts = *x509.NewCertPool()
	if *serverCert != "" {
		CertPoolAppendCertsFromFile(&runOptions.trustedCerts, *serverCert)
	}

	numTrusted := len(runOptions.trustedCerts.Subjects())
	if *bootstrap != "" && numTrusted == 0 {
		log.Warnln("No server certificate is trusted," +
			" use -trusted-certs with a proper certificate")
	}

	var haveCert bool = false
	clientCert, err := tls.LoadX509KeyPair(*certFile, *certKey)
	if err != nil {
		log.Warnln("Failed to load certificate and key from files:",
			*certFile, *certKey)
	} else {
		runOptions.clientCert = clientCert
		haveCert = true
	}

	if *bootstrap != "" && !haveCert {
		log.Warnln("No client certificate is provided," +
			"use options -certificate and -cert-key")
	}

	return runOptions, nil
}

func doMain(args []string) error {
	runOptions, err := argsParse(args)
	if err != nil {
		return err
	}

	if runOptions.imageFile != "" {
		if err := doRootfs(runOptions.imageFile); err != nil {
			return err
		}
	}
	if runOptions.committing {
		if err := doCommitRootfs(); err != nil {
			return err
		}
	}
	if runOptions.bootstrap != "" {
		err := doBootstrap(runOptions.bootstrap,
			runOptions.trustedCerts,
			runOptions.clientCert)
		return err
	}

	return nil
}

func main() {
	if err := doMain(os.Args[1:]); err != nil && err != flag.ErrHelp {
		log.Errorln(err.Error())
		os.Exit(1)
	}
}
