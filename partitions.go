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
	"fmt"
	"os"
	"path"
	"strings"
	"syscall"

	"github.com/mendersoftware/log"
)

var (
	InvalidActivePartition         = errors.New("Invalid active partition")
	RootPartitionDoesNotMatchMount = errors.New("Can not match active partition and any of mounted devices.")
	ErrorNoMatchBootPartRootPart   = errors.New("No match between boot and root partitions.")
)

type PatririonGetter interface {
	GetInactive() (string, error)
	GetActive() (string, error)
}

type partitions struct {
	StatCommander
	BootEnvReadWriter
	mountBase           string
	active              string
	inactive            string
	blockDevSizeGetFunc func(file *os.File) (uint64, error)
}

func (p *partitions) GetInactive() (string, error) {
	if p.inactive != "" {
		log.Debug("Inactive partition: ", p.inactive)
		return p.inactive, nil
	}
	return p.getAndSetInactivePartition()
}

func (p *partitions) GetActive() (string, error) {
	if p.active != "" {
		log.Debug("Active partition: ", p.active)
		return p.active, nil
	}
	return p.getAndSetActivePartition(isMountedRoot, getAllMountedDevices)
}

func (p *partitions) getPartitionSize(partition string) (int64, error) {
	// Size check on partition: Don't try to write into a partition which is
	// smaller than the image file.
	var partSize uint64

	partFd, err := os.OpenFile(partition, os.O_WRONLY, 0)
	if err != nil {
		return 0, fmt.Errorf("Not able to open partition: %s: %s\n",
			partition, err.Error())
	}
	defer partFd.Close()

	partSize, err = p.blockDevSizeGetFunc(partFd)
	if err == NotABlockDevice {
		partInfo, err := p.Stat(partition)
		if err != nil {
			return 0, fmt.Errorf("Unable to stat() partition: %s: %s\n", partition, err.Error())
		}
		return partInfo.Size(), nil
	} else if err != nil {
		return 0, fmt.Errorf("Unable to determine size of partition "+
			"%s: %s", partition, err.Error())
	} else {
		return int64(partSize), nil
	}
}

func (p *partitions) getAndSetInactivePartition() (string, error) {
	active, err := p.GetActive()
	if err != nil {
		return "", err
	}

	mountSufix := active[len(active)-1:]
	mountPrefix := active[:len(active)-1]

	log.Debugf("Setting inactive partition %s [%s][%s]", active, mountSufix, mountPrefix)

	switch mountSufix {
	case "2":
		p.inactive = mountPrefix + "3"
		return p.inactive, nil
	case "3":
		p.inactive = mountPrefix + "2"
		return p.inactive, nil
	default:
		log.Error("Can not parse active partition string: ", active)
		return "", InvalidActivePartition
	}
}

func getRootCandidateFromMount(data []byte) string {
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Split(line, " ")
		if len(fields) >= 3 && fields[2] == "/" {
			// we just need the first one (in fact there should be ONLY one)
			return fields[0]
		}
	}
	return ""
}

func getRootDevice(sc StatCommander) *syscall.Stat_t {
	rootStat, err := sc.Stat("/")
	if err != nil {
		// Seriously??
		// Something is *very* wrong.
		log.Error("Can not stat root device.")
		return nil
	}
	return rootStat.Sys().(*syscall.Stat_t)
}

// There is a lot of system calls here so will be rather hard to test
func getAllMountedDevices(mountBase string) (names []string, err error) {
	devDir := path.Dir(mountBase)
	devFd, err := os.Open(devDir)
	if err != nil {
		return nil, err
	}
	defer devFd.Close()

	return devFd.Readdirnames(0)
}

// There is a lot of system calls here so will be rather hard to test
func isMountedRoot(sc StatCommander, dev string, root *syscall.Stat_t) bool {
	// Check if this is a device file and its device ID matches that of the
	// root directory.
	stat, err := sc.Stat(dev)
	if err != nil ||
		(stat.Mode()&os.ModeDevice) == 0 ||
		stat.Sys().(*syscall.Stat_t).Rdev != root.Dev {
		return false
	}

	return true
}

func getRootFromMountedDevices(sc StatCommander,
	rootChecker func(StatCommander, string, *syscall.Stat_t) bool,
	mountBase string, devices []string, root *syscall.Stat_t) string {

	for _, device := range devices {
		deviceFullPath := path.Join(mountBase, device)
		if rootChecker(sc, deviceFullPath, root) {
			return deviceFullPath
		}
	}
	return ""
}

func (p *partitions) getAndSetActivePartition(rootChecker func(StatCommander, string, *syscall.Stat_t) bool,
	getMountedDevices func(string) ([]string, error)) (string, error) {
	mountData, err := p.Command("mount").Output()
	if err != nil {
		return "", err
	}

	mountCandidate := getRootCandidateFromMount(mountData)
	rootDevice := getRootDevice(p)
	if rootDevice == nil {
		return "", errors.New("Can not find root device")
	}

	// First check if mountCandidate matches rootDevice
	if mountCandidate != "" {
		if rootChecker(p, mountCandidate, rootDevice) {
			p.active = mountCandidate
			log.Debugf("Setting active partition from mount candidate: %s", p.active)
			return p.active, nil
		}
		// If not see if we are lucky somewhere else
	}

	mountedDevices, err := getMountedDevices(p.mountBase)
	activePartition := getRootFromMountedDevices(p, rootChecker, p.mountBase, mountedDevices, rootDevice)

	if activePartition == "" {
		return "", RootPartitionDoesNotMatchMount
	}

	bootEnvBootPart, err := getBootEnvActivePartition(p.BootEnvReadWriter)
	if err != nil {
		return "", err
	}
	if checkBootEnvAndRootPartitionMatch(bootEnvBootPart, activePartition) {
		p.active = activePartition
		log.Debug("Setting active partition: ", activePartition)
		return p.active, nil
	}

	log.Error("Mounted root '" + activePartition + "' does not match boot enviromnent boot_part: " + bootEnvBootPart)
	return "", ErrorNoMatchBootPartRootPart
}

func getBootEnvActivePartition(env BootEnvReadWriter) (string, error) {
	bootEnv, err := env.ReadEnv("boot_part")
	if err != nil {
		log.Error(err)
		return "", ErrorNoMatchBootPartRootPart
	}

	return bootEnv["boot_part"], nil
}

func checkBootEnvAndRootPartitionMatch(bootPartNum string, rootPart string) bool {
	return strings.HasSuffix(rootPart, bootPartNum)
}
