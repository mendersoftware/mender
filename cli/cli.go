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
	"io"
	"io/ioutil"
	"os"
	"os/exec"
	"path"
	"runtime"
	"strings"

	"github.com/mendersoftware/mender/app"
	"github.com/mendersoftware/mender/conf"
	"github.com/mendersoftware/mender/installer"
	mender_syslog "github.com/mendersoftware/mender/log/syslog"
	"github.com/mendersoftware/mender/system"
	log "github.com/sirupsen/logrus"
	"log/syslog"

	"github.com/pkg/errors"
	"github.com/urfave/cli/v2"
	"golang.org/x/crypto/ssh/terminal"
)

var (
	deprecatedCommandArgs = [...]string{"-install", "-commit", "-rollback", "-daemon",
		"-bootstrap", "-check-update", "-send-inventory", "-show-artifact"}
	deprecatedFlagArgs = [...]string{"-version", "-config", "-fallback-config",
		"-trusted-certs", "-forcebootstrap", "-skipverify", "-log-level",
		"-log-modules", "-no-syslog", "-log-file"}
	errDumpTerminal = errors.New("Refusing to write to terminal")
)

const (
	appDescription = "" +
		"mender integrates both the mender daemon and commands " +
		"for manually performing tasks performed by the daemon " +
		"(see list of COMMANDS below).\n\n" +
		"Global flag remarks:\n" +
		"  - Supported log levels incudes: 'debug', 'info', " +
		"'warning', 'error', 'panic' and 'fatal'.\n"
	snapshotDescription = "Creates a snapshot of the currently running " +
		"rootfs. The snapshots can be passed as a rootfs-image to the " +
		"mender-artifact tool to create an update based on THIS " +
		"device's rootfs. Refer to the list of COMMANDS to specify " +
		"where to stream the image.\n" +
		"\t NOTE: If the process gets killed (e.g. by SIGKILL) " +
		"while a snapshot is in progress, the system may freeze - " +
		"forcing you to manually hard-reboot the device. " +
		"Use at your own risk - preferably on a device that " +
		"is physically accessible."
	snapshotDumpDescription = "Dump rootfs to standard out. Exits if " +
		"output isn't redirected."
)

const (
	errMsgAmbiguousArgumentsGivenF = "Ambiguous arguments given - " +
		"unrecognized argument: %s"
	errMsgConflictingArgumentsF = "Conflicting arguments given, only one " +
		"of the following flags may be given: {%q, %q}"
)

func ShowVersion() string {
	return fmt.Sprintf("%s\truntime: %s",
		conf.VersionString(), runtime.Version())
}

func transformDeprecatedArgs(args []string) []string {
	argInSlice := func(arg string, slice []string) bool {
		for _, s := range slice {
			if arg == s {
				return true
			}
		}
		return false
	}
	for i := 0; i < len(args); i++ {
		if argInSlice(args[i], deprecatedCommandArgs[:]) {
			// Remove hyphen
			args[i] = args[i][1:]
		} else if argInSlice(args[i], deprecatedFlagArgs[:]) {
			// Prepend hyphen
			args[i] = "-" + args[i]
		} else if args[i] == "-info" {
			// replace with log-level flags
			args = append(args[:i],
				append([]string{"--log-level", "info"},
					args[i+1:]...)...)
		} else if args[i] == "-debug" {
			// replace with log-level flags
			args = append(args[:i],
				append([]string{"--log-level", "debug"},
					args[i+1:]...)...)
		}
	}
	return args
}

func SetupCLI(args []string) error {
	runOptions := &runOptionsType{}

	// Filter commandline arguments for backwards compatibility.
	// FIXME: Remove argument filtering in Mender v3.0
	args = transformDeprecatedArgs(args)

	app := &cli.App{
		Before:      runOptions.handleLogFlags,
		Description: appDescription,
		Name:        "mender",
		Usage:       "manage and start the Mender client.",
		Version:     ShowVersion(),
	}
	app.Commands = []*cli.Command{
		{
			Name:  "bootstrap",
			Usage: "Perform bootstrap and exit.",
			Flags: []cli.Flag{
				&cli.BoolFlag{
					Name:        "forcebootstrap",
					Aliases:     []string{"F"},
					Usage:       "Force bootstrap.",
					Destination: &runOptions.bootstrapForce},
			},
			Action: runOptions.handleCLIOptions,
		},
		{
			Name:  "check-update",
			Usage: "Force update check.",
			Action: func(_ *cli.Context) error {
				return updateCheck(
					exec.Command("kill", "-USR1"),
					exec.Command("systemctl",
						"show", "-p",
						"MainPID", "mender-client"))
			},
		},
		{
			Name: "commit",
			Usage: "Commit current Artifact. Returns (2) " +
				"if no update in progress.",
			Action: runOptions.handleCLIOptions,
		},
		{
			Name:   "daemon",
			Usage:  "Start the client as a background service.",
			Action: runOptions.handleCLIOptions,
		},
		{
			Name: "install",
			Usage: "Mender Artifact to install - " +
				"local file or a `URL`.",
			ArgsUsage: "<IMAGEURL>",
			Action: func(ctx *cli.Context) error {
				runOptions.imageFile = ctx.Args().First()
				if len(runOptions.imageFile) == 0 {
					cli.ShowAppHelpAndExit(ctx, 1)
				}
				return runOptions.handleCLIOptions(ctx)
			},
		},
		{
			Name: "rollback",
			Usage: "Rollback current Artifact. Returns (2) " +
				"if no update in progress.",
			Action: runOptions.handleCLIOptions,
		},
		{
			Name:  "send-inventory",
			Usage: "Force inventory update.",
			Action: func(_ *cli.Context) error {
				return updateCheck(
					exec.Command("kill", "-USR2"),
					exec.Command("systemctl",
						"show", "-p",
						"MainPID", "mender-client"))
			},
		},
		{
			Name: "setup",
			Usage: "Perform configuration setup - " +
				"'mender setup --help' for command options.",
			ArgsUsage: "[options]",
			Action:    runOptions.setupCLIHandler,
			Flags: []cli.Flag{
				&cli.StringFlag{
					Name:        "config",
					Aliases:     []string{"c"},
					Destination: &runOptions.setupOptions.configPath,
					Value:       conf.DefaultConfFile,
					Usage:       "`PATH` to configuration file."},
				&cli.StringFlag{
					Name:    "data",
					Aliases: []string{"d"},
					Usage:   "Mender state data `DIR`ECTORY path.",
					Value:   conf.DefaultDataStore},
				&cli.StringFlag{
					Name:        "device-type",
					Destination: &runOptions.setupOptions.deviceType,
					Usage:       "Name of the device `type`."},
				&cli.StringFlag{
					Name:        "username",
					Destination: &runOptions.setupOptions.username,
					Usage:       "User `E-Mail` at hosted.mender.io."},
				&cli.StringFlag{
					Name:        "password",
					Destination: &runOptions.setupOptions.password,
					Usage:       "User `PASSWORD` at hosted.mender.io."},
				&cli.StringFlag{
					Name:        "server-url",
					Aliases:     []string{"url"},
					Destination: &runOptions.setupOptions.serverURL,
					Usage:       "`URL` to Mender server.",
					Value:       "https://docker.mender.io"},
				&cli.StringFlag{
					Name:        "server-ip",
					Destination: &runOptions.setupOptions.serverIP,
					Usage:       "Server ip address."},
				&cli.StringFlag{
					Name:        "server-cert",
					Aliases:     []string{"E"},
					Destination: &runOptions.setupOptions.serverCert,
					Usage:       "`PATH` to trusted server certificates"},
				&cli.StringFlag{
					Name:        "tenant-token",
					Destination: &runOptions.setupOptions.tenantToken,
					Usage:       "Hosted Mender tenanant `token`"},
				&cli.IntFlag{
					Name:        "inventory-poll",
					Destination: &runOptions.setupOptions.invPollInterval,
					Usage:       "Inventory poll interval in `sec`onds."},
				&cli.IntFlag{
					Name:        "retry-poll",
					Destination: &runOptions.setupOptions.retryPollInterval,
					Usage:       "Retry poll interval in `sec`onds."},
				&cli.IntFlag{
					Name:        "update-poll",
					Destination: &runOptions.setupOptions.updatePollInterval,
					Usage:       "Update poll interval in `sec`onds."},
				&cli.BoolFlag{
					Name:        "hosted-mender",
					Destination: &runOptions.setupOptions.hostedMender,
					Usage:       "Setup device towards Hosted Mender."},
				&cli.BoolFlag{
					Name:        "demo",
					Destination: &runOptions.setupOptions.demo,
					Usage:       "Use demo configuration."},
				&cli.BoolFlag{
					Name:  "quiet",
					Usage: "Suppress informative prompts."},
			},
		},
		{
			Name: "snapshot",
			Usage: "Create filesystem snapshot -" +
				"'mender snapshot --help' for more.",
			Description: snapshotDescription,
			Subcommands: []*cli.Command{
				{
					Name:        "dump",
					Description: snapshotDumpDescription,
					Usage:       "Dumps rootfs to stdout.",
					Action:      runOptions.DumpSnapshot,
					Flags: []cli.Flag{
						&cli.StringFlag{
							Name: "source",
							Usage: "Path to target " +
								"filesystem " +
								"file/directory/device" +
								"to snapshot.",
							Value: "/"},
						&cli.BoolFlag{
							Name:    "quiet",
							Aliases: []string{"q"},
							Usage: "Suppress output " +
								"and only report " +
								"logs from " +
								"ERROR level",
						},
						&cli.StringFlag{
							Name:    "compression",
							Aliases: []string{"C"},
							Usage: "Compression type to use on the" +
								"rootfs snapshot {none,gzip}",
							Value: "none",
						},
					},
				},
			},
		},
		{
			Name: "show-artifact",
			Usage: "Print the current artifact name to the " +
				"command line and exit.",
			Action: func(ctx *cli.Context) error {
				if !ctx.IsSet("log-level") {
					log.SetLevel(log.DebugLevel)
				}
				return runOptions.handleCLIOptions(ctx)
			},
		},
	}
	app.Flags = []cli.Flag{
		&cli.StringFlag{
			Name:        "config",
			Aliases:     []string{"c"},
			Usage:       "Configuration `FILE` path.",
			Value:       conf.DefaultConfFile,
			Destination: &runOptions.config},
		&cli.StringFlag{
			Name:        "fallback-config",
			Aliases:     []string{"b"},
			Usage:       "Fallback configuration `FILE` path.",
			Value:       conf.DefaultFallbackConfFile,
			Destination: &runOptions.fallbackConfig},
		&cli.StringFlag{
			Name:        "data",
			Aliases:     []string{"d"},
			Usage:       "Mender state data `DIR`ECTORY path.",
			Value:       conf.DefaultDataStore,
			Destination: &runOptions.dataStore},
		&cli.StringFlag{
			Name:        "log-file",
			Aliases:     []string{"L"},
			Usage:       "`FILE` to log to.",
			Destination: &runOptions.logOptions.logFile},
		&cli.StringFlag{
			Name:        "log-level",
			Aliases:     []string{"l"},
			Usage:       "Set logging `level`.",
			Value:       "info",
			Destination: &runOptions.logOptions.logLevel},
		&cli.StringFlag{
			Name:    "log-modules",
			Aliases: []string{"m"},
			Usage: "-log-modules is accepted for compatibility " +
				"but has no effect",
			Destination: &runOptions.logOptions.logModules},
		&cli.StringFlag{
			Name:        "trusted-certs",
			Aliases:     []string{"E"},
			Usage:       "Trusted server certificates `FILE` path.",
			Destination: &runOptions.Config.ServerCert},
		&cli.BoolFlag{
			Name:        "forcebootstrap",
			Aliases:     []string{"F"},
			Usage:       "Force bootstrap.",
			Destination: &runOptions.bootstrapForce},
		&cli.BoolFlag{
			Name:        "no-syslog",
			Usage:       "Disable logging to syslog.",
			Destination: &runOptions.logOptions.noSyslog},
		&cli.BoolFlag{
			Name:        "skipverify",
			Usage:       "Skip certificate verification.",
			Destination: &runOptions.Config.NoVerify},
	}
	cli.HelpPrinter = upgradeHelpPrinter(cli.HelpPrinter)
	cli.VersionPrinter = func(c *cli.Context) {
		fmt.Fprintf(c.App.Writer, "%s\n", ShowVersion())
	}
	return app.Run(args)
}

func (runOptions *runOptionsType) commonCLIHandler(
	ctx *cli.Context) (*conf.MenderConfig,
	installer.DualRootfsDevice, error) {

	log.SetLevel(log.DebugLevel)
	if ctx.Command.Name != "install" && ctx.Args().Len() > 0 {
		return nil, nil, errors.Errorf(
			errMsgAmbiguousArgumentsGivenF,
			ctx.Args().First())
	}

	// Handle config flags
	config, err := conf.LoadConfig(
		runOptions.config, runOptions.fallbackConfig)
	if err != nil {
		return nil, nil, err
	}
	if runOptions.Config.NoVerify {
		config.HttpsClient.SkipVerify = true
	}

	env := installer.NewEnvironment(new(system.OsCalls))

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
	return config, dualRootfsDevice, nil
}

func (runOptions *runOptionsType) handleCLIOptions(ctx *cli.Context) error {
	config, dualRootfsDevice, err := runOptions.commonCLIHandler(ctx)
	if err != nil {
		return err
	}

	app.DeploymentLogger = app.NewDeploymentLogManager(runOptions.dataStore)

	// Execute commands
	switch ctx.Command.Name {

	case "show-artifact",
		"install",
		"commit",
		"rollback":
		return handleArtifactOperations(ctx, *runOptions, dualRootfsDevice, config)

	case "bootstrap":
		return doBootstrapAuthorize(config, runOptions)

	case "daemon":
		d, err := initDaemon(config, dualRootfsDevice, runOptions)
		if err != nil {
			return err
		}
		defer d.Cleanup()
		return runDaemon(d)
	case "setup":
		// Check that user has permission to directories so that
		// the user doesn't have to perform the setup before raising
		// an error.
		if err = checkWritePermissions(path.Dir(runOptions.config)); err != nil {
			return err
		}
		if err = checkWritePermissions(runOptions.dataStore); err != nil {
			return err
		}
		// Make sure that device_type file is consistent
		// with flag options.
		config.MenderConfigFromFile.DeviceTypeFile = path.Join(
			runOptions.dataStore, "device_type")
		// Run cli setup prompts.

		if err := doSetup(ctx, &config.MenderConfigFromFile,
			&runOptions.setupOptions); err != nil {
			return err
		}
		if !ctx.Bool("quiet") {
			fmt.Println(promptDone)
		}

	default:
		cli.ShowAppHelpAndExit(ctx, 1)
	}
	return err
}

func (runOptions *runOptionsType) setupCLIHandler(ctx *cli.Context) error {
	if ctx.Args().Len() > 0 {
		return errors.Errorf(
			errMsgAmbiguousArgumentsGivenF,
			ctx.Args().First())
	}
	if !ctx.IsSet("log-level") {
		log.SetLevel(log.WarnLevel)
	}
	if err := runOptions.setupOptions.handleImplicitFlags(ctx); err != nil {
		return err
	}

	// Handle overlapping global flags
	if ctx.IsSet("config") && !ctx.IsSet("config") {
		runOptions.setupOptions.configPath = runOptions.config
	} else {
		runOptions.config = runOptions.setupOptions.configPath
	}
	if ctx.IsSet("data") {
		runOptions.dataStore = ctx.String("data")
	}
	if runOptions.Config.ServerCert != "" &&
		runOptions.setupOptions.serverCert == "" {
		runOptions.setupOptions.serverCert = runOptions.Config.ServerCert
	} else {
		runOptions.Config.ServerCert = runOptions.setupOptions.serverCert
	}
	return runOptions.handleCLIOptions(ctx)
}

func upgradeHelpPrinter(defaultPrinter func(w io.Writer, templ string, data interface{})) func(
	w io.Writer, templ string, data interface{}) {
	// Applies the ordinary help printer with column post processing
	return func(stdout io.Writer, templ string, data interface{}) {
		// Need at least 10 characters for lastr column in order to
		// pretty print; otherwise the output is unreadable.
		const minColumnWidth = 10
		isLowerCase := func(c rune) bool {
			// returns true if c in [a-z] else false
			asciiVal := int(c)
			if asciiVal >= 0x61 && asciiVal <= 0x7A {
				return true
			}
			return false
		}
		// defaultPrinter parses the text-template and outputs to buffer
		var buf bytes.Buffer
		defaultPrinter(&buf, templ, data)
		terminalWidth, _, err := terminal.GetSize(int(os.Stdout.Fd()))
		if err != nil {
			// Just write help as is.
			stdout.Write(buf.Bytes())
			return
		}
		for line, err := buf.ReadString('\n'); err == nil; line, err = buf.ReadString('\n') {
			if len(line) <= terminalWidth+1 {
				stdout.Write([]byte(line))
				continue
			}
			newLine := line
			indent := strings.LastIndex(
				line[:terminalWidth], "  ")
			// find indentation of last column
			if indent == -1 {
				indent = 0
			}
			indent += strings.IndexFunc(
				strings.ToLower(line[indent:]), isLowerCase) - 1
			if indent >= terminalWidth-minColumnWidth ||
				indent == -1 {
				indent = 0
			}
			// Format the last column to be aligned
			for len(newLine) > terminalWidth {
				// find word to insert newline
				idx := strings.LastIndex(newLine[:terminalWidth], " ")
				if idx == indent || idx == -1 {
					idx = terminalWidth
				}
				stdout.Write([]byte(newLine[:idx] + "\n"))
				newLine = newLine[idx:]
				newLine = strings.Repeat(" ", indent) + newLine
			}
			stdout.Write([]byte(newLine))
		}
		if err != nil {
			log.Fatalf("CLI HELP: error writing help string: %v\n", err)
		}
	}
}

func (runOptions *runOptionsType) handleLogFlags(ctx *cli.Context) error {
	// Handle log options
	level, err := log.ParseLevel(runOptions.logOptions.logLevel)
	if err != nil {
		return err
	}
	log.SetLevel(level)

	if level == log.DebugLevel {
		// Add the 'func' field to the logger to improve debug log messages
		log.SetReportCaller(true)
	}

	if ctx.IsSet("log-file") {
		fd, err := os.Create(runOptions.logOptions.logFile)
		if err != nil {
			return err
		}
		log.SetOutput(fd)
	}
	if ctx.IsSet("no-syslog") &&
		!runOptions.logOptions.noSyslog {
		hook, err := mender_syslog.NewSyslogHook(
			"", "", syslog.LOG_DEBUG|syslog.LOG_USER, "mender", level)
		if err != nil {
			log.Warnf("Could not connect to syslog daemon: %s. "+
				"(use -no-syslog to disable completely)",
				err.Error())
		} else {
			log.AddHook(hook)
		}
	}
	return nil
}

func checkWritePermissions(dir string) error {
	_, err := os.Stat(dir)
	if os.IsNotExist(err) {
		err := os.MkdirAll(dir, 0755)
		if err != nil {
			return errors.Wrapf(err, "Error creating "+
				"directory %q", dir)
		}
	} else if os.IsPermission(err) {
		return errors.Wrapf(os.ErrPermission,
			"Error trying to stat directory %q", dir)
	} else if err != nil {
		return errors.Errorf("Error trying to stat directory %q", dir)
	}
	f, err := ioutil.TempFile(dir, "temporaryFile")
	if os.IsPermission(err) {
		return errors.Wrapf(err, "User does not have "+
			"permission to write to data store "+
			"directory %q", dir)
	} else if err != nil {
		return errors.Wrapf(err,
			"Error checking write permissions to "+
				"directory %q", dir)
	}
	os.Remove(f.Name())
	return nil
}
