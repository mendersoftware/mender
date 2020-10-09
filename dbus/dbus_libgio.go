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
// +build !nodbus,cgo

package dbus

// #cgo pkg-config: gio-2.0
// #include <gio/gio.h>
import "C"

type dbusAPILibGio struct{}

// GenerateGUID generates a D-Bus GUID that can be used with e.g. g_dbus_connection_new().
// https://developer.gnome.org/gio/stable/gio-D-Bus-Utilities.html#g-dbus-generate-guid
func (d *dbusAPILibGio) GenerateGUID() string {
	guid := C.g_dbus_generate_guid()
	defer C.g_free(C.gpointer(guid))
	return goString(guid)
}

// IsGUID Checks if string is a D-Bus GUID.
// https://developer.gnome.org/gio/stable/gio-D-Bus-Utilities.html#g-dbus-is-guid
func (d *dbusAPILibGio) IsGUID(str string) bool {
	cstr := C.CString(str)
	return goBool(C.g_dbus_is_guid(cstr))
}

func init() {
	dbusAPI = &dbusAPILibGio{}
}
