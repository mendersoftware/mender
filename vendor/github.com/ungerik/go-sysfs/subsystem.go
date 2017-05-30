package sysfs

import (
// "os"
)

type Subsystem string

func (subsys Subsystem) Exists() bool {
	return dirExists(string(subsys))
}

func (subsys Subsystem) Name() string {
	return string(subsys)[5:]
}

func (subsys Subsystem) Objects() []Object {
	path := string(subsys) + "/"
	objects := make([]Object, 0)
	lsDirs(path, func(name string) {
		objects = append(objects, Object(path+name))
	})
	return objects
}

func (subsys Subsystem) Object(name string) Object {
	return Object(string(subsys) + "/" + name)
}
