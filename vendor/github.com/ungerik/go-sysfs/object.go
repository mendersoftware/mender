package sysfs

import (
	// "os"
	"strings"
)

type Object string

func (obj Object) Exists() bool {
	return dirExists(string(obj))
}

func (obj Object) Name() string {
	return string(obj)[strings.LastIndex(string(obj), "/")+1:]
}

func (obj Object) SubObjects() []Object {
	path := string(obj) + "/"
	objects := make([]Object, 0)
	lsDirs(path, func(name string) {
		objects = append(objects, Object(path+name))
	})
	return objects
}

func (obj Object) SubObject(name string) Object {
	return Object(string(obj) + "/" + name)
}

func (obj Object) Attributes() []Attribute {
	path := string(obj) + "/"
	attribs := make([]Attribute, 0)
	lsFiles(path, func(name string) {
		attribs = append(attribs, Attribute{Path: path + name})
	})
	return attribs
}

func (obj Object) Attribute(name string) *Attribute {
	return &Attribute{Path: string(obj) + "/" + name}
}
