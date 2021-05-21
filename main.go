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

package main

import (
	"os"
	"os/signal"
	"syscall"

	"github.com/mendersoftware/mender/app"
	"github.com/mendersoftware/mender/cli"
	"github.com/mendersoftware/mender/installer"
	log "github.com/sirupsen/logrus"
)

func init() {
	// SIGUSR1 forces an update check.
	// SIGUSR2 forces an inventory update.
	// SIGTERM marks the exit.
	signal.Notify(cli.SignalHandlerChan, syscall.SIGUSR1, syscall.SIGUSR2, syscall.SIGTERM)
}

func doMain() int {
	if err := cli.SetupCLI(os.Args); err != nil {
		switch err {
		case cli.ErrSIGTERM:
			log.Infoln(err.Error())
			return 0
		case app.ErrorManualRebootRequired:
			return 4
		case installer.ErrorNothingToCommit:
			log.Warnln(err.Error())
			return 2
		default:
			log.Errorln(err.Error())
			return 1
		}
	}
	return 0
}

func main() {
	os.Exit(doMain())
}
