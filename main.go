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
import "fmt"
import "flag"
import "os"

type runOptionsType struct {
	imageFile  string
	committing bool
}

var errMsgNoArgumentsGiven error = errors.New("Must give either -rootfs or -commit")

func argsParse(args []string) (runOptionsType, error) {
	var runOptions runOptionsType

	parsing := flag.NewFlagSet("mender", flag.ContinueOnError)

	imageFile := parsing.String("rootfs", "",
		"Root filesystem image file to use for update")
	committing := parsing.Bool("commit", false, "Commit current update")
	if err := parsing.Parse(args); err != nil {
		return runOptions, err
	}

	if *imageFile == "" && !*committing {
		return runOptions, errMsgNoArgumentsGiven
	}

	runOptions.imageFile = *imageFile
	runOptions.committing = *committing

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

	return nil
}

func main() {
	if err := doMain(os.Args[1:]); err != nil {
		fmt.Println(err.Error())
		os.Exit(1)
	}
}
