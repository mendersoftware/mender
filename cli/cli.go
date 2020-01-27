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
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path"
	"runtime"
	"strings"

	"github.com/mendersoftware/log"
	"github.com/mendersoftware/mender/app"
	"github.com/mendersoftware/mender/conf"
	"github.com/mendersoftware/mender/installer"
	"github.com/mendersoftware/mender/system"

	"github.com/alfrunes/cli"
	"github.com/pkg/errors"
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
		"for manually performing tasks performed by the daemon."
	setupDescription = "Interactive setup of the mender configuration " +
		"file (mender.conf). If a configuration is already present" +
		"only the values entered during the setup will be updated, " +
		"otherwise a new file is generated."
	snapshotDescription = "Creates a snapshot of the currently running " +
		"rootfs. The snapshots can be passed as a rootfs-image to the " +
		"mender-artifact tool to create an update based on THIS " +
		"device's rootfs. Refer to the list of COMMANDS to specify " +
		"where to stream the image.\n" +
		"NOTE: If the process gets killed (e.g. by SIGKILL) " +
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
	// Filter commandline arguments for backwards compatibility.
	// FIXME: Remove argument filtering in Mender v3.0
	args = transformDeprecatedArgs(args)

	app := &cli.App{
		Description: appDescription,
		Name:        "mender",
	}
	app.Commands = []*cli.Command{
		{
			Name:  "bootstrap",
			Usage: "Perform bootstrap and exit.",
			Flags: []*cli.Flag{
				{
					Name:  "forcebootstrap, F",
					Usage: "Force bootstrap.",
				},
			},
			Action: handleCLIOptions,
		},
		{
			Name:  "check-update",
			Usage: "Force update check.",
			Action: func(ctx *cli.Context) error {
				if len(ctx.GetPositionals()) > 0 {
					return errors.Errorf(
						errMsgAmbiguousArgumentsGivenF,
						ctx.GetPositionals())
				}
				return updateCheck(
					exec.Command("kill", "-USR1"),
					exec.Command("systemctl",
						"show", "-p",
						"MainPID", "mender"))
			},
		},
		{
			Name: "commit",
			Usage: "Commit current Artifact. Returns (2) " +
				"if no update in progress.",
			Action: handleCLIOptions,
		},
		{
			Name:   "daemon",
			Usage:  "Start the client as a background service.",
			Action: handleCLIOptions,
		},
		{
			Name: "install",
			Usage: "Mender Artifact to install - " +
				"local file or a `URL`.",
			PositionalArguments: []string{"<IMAGEURL>"},
			Action:              handleCLIOptions,
		},
		{
			Name: "rollback",
			Usage: "Rollback current Artifact. Returns (2) " +
				"if no update in progress.",
			Action: handleCLIOptions,
		},
		{
			Name:  "send-inventory",
			Usage: "Force inventory update.",
			Action: func(ctx *cli.Context) error {
				if args := ctx.GetPositionals(); len(args) > 0 {
					return errors.Errorf(
						errMsgAmbiguousArgumentsGivenF,
						args)
				}
				return updateCheck(
					exec.Command("kill", "-USR2"),
					exec.Command("systemctl",
						"show", "-p",
						"MainPID", "mender"))
			},
		},
		{
			Name:               "setup",
			Description:        setupDescription,
			Usage:              "Setup the client configuration parameters.",
			Action:             setupCLIHandler,
			InheritParentFlags: true,
			Flags: []*cli.Flag{
				{ // inherits config, data, trusted-certs
					Name:    "device-type",
					MetaVar: "name",
					Usage:   "Name of the device type."},
				{
					Name:    "server-url",
					MetaVar: "URL",
					Usage:   "URL to Mender server.",
					Default: defaultServerURL},
				{
					Name:    "server-ip",
					Usage:   "Server ip address.",
					Default: defaultServerIP},
				{
					Name:    "tenant-token",
					MetaVar: "JWT",
					Usage:   "Hosted Mender (JWT) tenant token"},
				{
					Name:    "username",
					MetaVar: "email",
					Usage:   "Username at hosted.mender.io."},
				{
					Name:    "password",
					MetaVar: "pass",
					Usage:   "User password at hosted.mender.io."},
				{
					Name:    "inventory-poll",
					Type:    cli.Int,
					MetaVar: "sec",
					Usage:   "Inventory poll interval in seconds.",
					Default: defaultInventoryPoll},
				{
					Name:    "retry-poll",
					Type:    cli.Int,
					MetaVar: "sec",
					Usage:   "Retry poll interval in seconds.",
					Default: defaultRetryPoll},
				{
					Name:    "update-poll",
					Type:    cli.Int,
					MetaVar: "sec",
					Usage:   "Update poll interval in seconds.",
					Default: defaultUpdatePoll},
				{
					Name:  "hosted-mender",
					Type:  cli.Bool,
					Usage: "Setup device towards Hosted Mender."},
				{
					Name:  "demo",
					Type:  cli.Bool,
					Usage: "Use demo configuration."},
				{
					Name:  "quiet",
					Char:  'q',
					Type:  cli.Bool,
					Usage: "Suppress informative prompts."},
			},
		},
		{
			Name:        "snapshot",
			Usage:       "Create a filesystem snapshot.",
			Description: snapshotDescription,
			Flags: []*cli.Flag{
				{
					Name: "source",
					Usage: "Path to filesystem " +
						"{file/directory/device} to " +
						"snapshot.",
					MetaVar: "path",
					Default: "/"},
				{
					Name: "compression",
					Char: 'C',
					Usage: "Compression type to use on " +
						"the rootfs snapshot ",
					Choices: []string{"gzip", "none"},
					MetaVar: "type",
					Default: "none",
				},
				{
					Name: "quiet",
					Char: 'q',
					Type: cli.Bool,
					Usage: "Suppress output and only " +
						"report logs from ERROR level",
				},
			},
			SubCommands: []*cli.Command{
				{
					Name:               "dump",
					Description:        snapshotDumpDescription,
					Usage:              "Dumps rootfs to stdout.",
					Action:             DumpSnapshot,
					InheritParentFlags: true,
				},
			},
		},
		{
			Name: "show-artifact",
			Usage: "Print the current artifact name to the " +
				"command line and exit.",
			Action: func(ctx *cli.Context) error {
				if _, isSet := ctx.String("log-level"); !isSet {
					log.SetLevel(log.WarnLevel)
				}
				return handleCLIOptions(ctx)
			},
		},
	}
	app.Flags = []*cli.Flag{
		{
			Name:    "config",
			Char:    'c',
			Usage:   "Configuration file path.",
			MetaVar: "file",
			Default: conf.DefaultConfFile},
		{
			Name:    "fallback-config",
			Char:    'b',
			Usage:   "Fallback configuration file path.",
			MetaVar: "file",
			Default: conf.DefaultFallbackConfFile},
		{
			Name:    "data",
			Char:    'd',
			Usage:   "Mender state data directory path.",
			MetaVar: "dir",
			Default: conf.DefaultDataStore},
		{
			Name:    "log-file",
			Char:    'L',
			MetaVar: "file",
			Usage:   "File to store logs."},
		{
			Name:    "log-level",
			Char:    'l',
			Usage:   "Set logging level.",
			MetaVar: "level",
			Default: "info",
			Choices: []string{"debug", "info", "warn",
				"error", "fatal", "panic"}},
		{
			Name:    "log-modules",
			Char:    'm',
			MetaVar: "list",
			Usage:   "Comma-separated list of logging modules."},
		{
			Name:    "trusted-certs",
			Char:    'E',
			MetaVar: "file",
			Usage:   "Path to chain of trusted server certificates."},
		{
			Name:  "forcebootstrap",
			Char:  'F',
			Type:  cli.Bool,
			Usage: "Force bootstrap."},
		{
			Name:  "no-syslog",
			Type:  cli.Bool,
			Usage: "Disable syslog (debug level is always disabled)."},
		{
			Name:  "skipverify",
			Type:  cli.Bool,
			Usage: "Skip certificate verification."},
		{
			Name:  "version",
			Type:  cli.Bool,
			Char:  'v',
			Usage: "Show application version"},
	}

	app.Action = func(ctx *cli.Context) error {
		if _, isSet := ctx.Bool("version"); isSet {
			fmt.Printf("%s\n", ShowVersion())
		} else {
			ctx.PrintHelp()
		}
		return nil
	}
	return app.Run(args)
}

func commonCLIHandler(ctx *cli.Context) (*conf.MenderConfig,
	installer.DualRootfsDevice, error) {

	args := ctx.GetPositionals()
	if len(ctx.Command.PositionalArguments) == 0 {
		if len(args) > 0 {
			return nil, nil, errors.Errorf(
				errMsgAmbiguousArgumentsGivenF,
				args)
		}
	} else if len(args) == 0 {
		ctx.PrintUsage()
		return nil, nil, errors.Errorf(
			"missing positional arguments: %v", args)
	}
	// Handle config flags
	configPath, _ := ctx.String("config")
	fallbackConfigPath, _ := ctx.String("fallback-config")
	config, err := conf.LoadConfig(
		configPath, fallbackConfigPath)
	if err != nil {
		return nil, nil, err
	}
	config.HttpsClient.SkipVerify, _ = ctx.Bool("skipverify")

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

func handleCLIOptions(ctx *cli.Context) error {
	config, dualRootfsDevice, err := commonCLIHandler(ctx)
	if err != nil {
		return err
	}

	configPath, _ := ctx.String("config")
	dataStore, _ := ctx.String("data")

	app.DeploymentLogger = app.NewDeploymentLogManager(dataStore)

	// Execute commands
	switch ctx.Command.Name {

	case "show-artifact",
		"install",
		"commit",
		"rollback":
		return handleArtifactOperations(ctx, dualRootfsDevice, config)

	case "bootstrap":
		forceBootstrap, _ := ctx.Bool("forcebootstrap")
		ctx.Free()
		return doBootstrapAuthorize(config, dataStore, forceBootstrap)

	case "daemon":
		d, err := initDaemon(ctx, config, dualRootfsDevice)
		if err != nil {
			return err
		}
		defer d.Cleanup()
		ctx.Free()
		return runDaemon(d)
	case "setup":
		// Check that user has permission to directories so that
		// the user doesn't have to perform the setup before raising
		// an error.
		if err = checkWritePermissions(path.Dir(configPath)); err != nil {
			return err
		}
		if err = checkWritePermissions(dataStore); err != nil {
			return err
		}
		// Make sure that device_type file is consistent
		// with flag options.
		config.MenderConfigFromFile.DeviceTypeFile = path.Join(
			dataStore, "device_type")
		// Run cli setup prompts.

		if err := doSetup(
			ctx, &config.MenderConfigFromFile); err != nil {
			return err
		}
		if quiet, _ := ctx.Bool("quiet"); !quiet {
			fmt.Println(promptDone)
		}

	default:
		ctx.PrintUsage()
	}
	return err
}

func setupCLIHandler(ctx *cli.Context) error {
	if args := ctx.GetPositionals(); len(args) > 0 {
		return errors.Errorf(errMsgAmbiguousArgumentsGivenF, args)
	}
	err := handleLogFlags(ctx)
	if err != nil {
		return err
	}

	if _, isSet := ctx.String("log-level"); isSet {
		log.SetLevel(log.WarnLevel)
	}
	if err := handleImplicitFlags(ctx); err != nil {
		return err
	}

	// Handle overlapping global flags
	return handleCLIOptions(ctx)
}

func handleLogFlags(ctx *cli.Context) error {
	// Handle log options
	logLevel, _ := ctx.String("log-level")
	level, err := log.ParseLevel(logLevel)
	if err != nil {
		return err
	}
	log.SetLevel(level)

	if logFile, isSet := ctx.String("log-file"); isSet {
		fd, err := os.Create(logFile)
		if err != nil {
			return err
		}
		log.SetOutput(fd)
	}
	if noSysLog, isSet := ctx.Bool("no-syslog"); isSet && noSysLog {
		if err := log.AddSyslogHook(); err != nil {
			log.Warnf("Could not connect to syslog daemon: %s. "+
				"(use -no-syslog to disable completely)",
				err.Error())
		}
	}
	if modules, isSet := ctx.String("modules"); isSet {
		modules := strings.Split(modules, ",")
		log.SetModuleFilter(modules)
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
