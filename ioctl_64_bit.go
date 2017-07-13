// Copyright 2017 Northern.tech AS
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

// +build amd64

package main

// Taken from <mtd/ubi-user.h>
const UBI_IOCVOLUP ioctlRequestValue = 0x40084f00

// Taken from <linux/fs.h>
const BLKSSZGET ioctlRequestValue = 0x00001268

// Taken from <sys/mount.h>
const BLKGETSIZE64 ioctlRequestValue = 0x80081272
