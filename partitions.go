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
	"os"
	"path"
	"strings"
	"syscall"

	"github.com/mendersoftware/log"
)

var (
	RootPartitionDoesNotMatchMount = errors.New("Can not match active partition and any of mounted devices.")
	ErrorNoMatchBootPartRootPart   = errors.New("No match between boot and root partitions.")
	ErrorPartitionNumberNotSet     = errors.New("RootfsPartA and RootfsPartB settings are not both set.")
	ErrorPartitionNumberSame       = errors.New("RootfsPartA and RootfsPartB cannot be set to the same value.")
	ErrorPartitionNoMatchActive    = errors.New("Active root partition matches neither RootfsPartA nor RootfsPartB.")
)

type partitions struct {
	StatCommander
	BootEnvReadWriter
	rootfsPartA string
	rootfsPartB string
	active      string
	inactive    string
}

func (p *partitions) GetInactive() (string, error) {
	if p.inactive != "" {
		log.Debug("Inactive partition: ", p.inactive)
		return p.inactive, nil
	}
	return p.getAndCacheInactivePartition()
}

func (p *partitions) GetActive() (string, error) {
	if p.active != "" {
		log.Debug("Active partition: ", p.active)
		return p.active, nil
	}
	return p.getAndCacheActivePartition(isMountedRoot, getAllMountedDevices)
}

func (p *partitions) getAndCacheInactivePartition() (string, error) {
	if p.rootfsPartA == "" || p.rootfsPartB == "" {
		return "", ErrorPartitionNumberNotSet
	}
	if p.rootfsPartA == p.rootfsPartB {
		return "", ErrorPartitionNumberSame
	}

	active, err := p.GetActive()
	if err != nil {
		return "", err
	}

	if active == p.rootfsPartA {
		p.inactive = p.rootfsPartB
	} else if active == p.rootfsPartB {
		p.inactive = p.rootfsPartA
	} else {
		return "", ErrorPartitionNoMatchActive
	}

	log.Debugf("Detected inactive partition %s, based on active partition %s", p.inactive, active)
	return p.inactive, nil
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

func getAllMountedDevices(devDir string) (names []string, err error) {
	devFd, err := os.Open(devDir)
	if err != nil {
		return nil, err
	}
	defer devFd.Close()

	names, err = devFd.Readdirnames(0)
	if err != nil {
		return nil, err
	}
	for i := 0; i < len(names); i++ {
		names[i] = path.Join(devDir, names[i])
	}

	return names, nil
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
	devices []string, root *syscall.Stat_t) (string, error) {

	for _, device := range devices {
		if rootChecker(sc, device, root) {
			return device, nil
		}
	}
	return "", RootPartitionDoesNotMatchMount
}

func (p *partitions) getAndCacheActivePartition(rootChecker func(StatCommander, string, *syscall.Stat_t) bool,
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

	const devDir string = "/dev"

	mountedDevices, err := getMountedDevices(devDir)
	if err != nil {
		return "", err
	}

	activePartition, err := getRootFromMountedDevices(p, rootChecker, mountedDevices, rootDevice)
	if err != nil {
		return "", err
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

	log.Error("Mounted root '" + activePartition + "' does not match boot environment mender_boot_part: " + bootEnvBootPart)
	return "", ErrorNoMatchBootPartRootPart
}

func getBootEnvActivePartition(env BootEnvReadWriter) (string, error) {
	bootEnv, err := env.ReadEnv("mender_boot_part")
	if err != nil {
		log.Error(err)
		return "", ErrorNoMatchBootPartRootPart
	}

	return bootEnv["mender_boot_part"], nil
}

func checkBootEnvAndRootPartitionMatch(bootPartNum string, rootPart string) bool {
	return strings.HasSuffix(rootPart, bootPartNum)
}
