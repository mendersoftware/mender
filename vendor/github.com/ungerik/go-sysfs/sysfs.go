package sysfs

import (
	"os"
)

var (
	Block       Subsystem = "/sys/block"
	Bus         Subsystem = "/sys/bus"
	Class       Subsystem = "/sys/class"
	Dev         Subsystem = "/sys/dev"
	Devices     Subsystem = "/sys/devices"
	Firmware    Subsystem = "/sys/firmware"
	FS          Subsystem = "/sys/fs"
	Hypervision Subsystem = "/sys/hypervisor"
	Kernel      Subsystem = "/sys/kernel"
	Module      Subsystem = "/sys/module"
	Power       Subsystem = "/sys/power"
)

// func exists(filename string) bool {
// 	_, err := os.Stat(filename)
// 	return err == nil
// }

func dirExists(dirname string) bool {
	info, err := os.Stat(dirname)
	return err == nil && info.IsDir()
}

func fileExists(dirname string) bool {
	info, err := os.Stat(dirname)
	return err == nil && !info.IsDir()
}

func lsFiles(dir string, callback func(name string)) error {
	f, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer f.Close()
	fileInfos, err := f.Readdir(-1)
	if err != nil {
		return err
	}
	for i := range fileInfos {
		if !fileInfos[i].IsDir() {
			callback(fileInfos[i].Name())
		}
	}
	return nil
}

func lsDirs(dir string, callback func(name string)) error {
	f, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer f.Close()
	fileInfos, err := f.Readdir(-1)
	if err != nil {
		return err
	}
	for i := range fileInfos {
		if fileInfos[i].IsDir() {
			callback(fileInfos[i].Name())
		}
	}
	return nil
}
