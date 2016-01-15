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

import "fmt"
import "os"
import "flag"

type argsType struct {
	imageFile  string
	committing bool
}

var args argsType

func argsParse() {
	imageFile := flag.String("rootfs", "", "Root filesystem image file to use for update")
	committing := flag.Bool("commit", false, "Commit current update")
	flag.Parse()

	if *imageFile == "" && !*committing {
		fmt.Printf("Must give either -rootfs or -commit\n")
		os.Exit(1)
	}

	args.imageFile = *imageFile
	args.committing = *committing
}

func main() {
	argsParse()

	if args.imageFile != "" {
		if err := doRootfs(args.imageFile); err != nil {
			fmt.Println(err.Error())
			os.Exit(1)
		}
	}
	if args.committing {
		doCommitRootfs()
	}
}
