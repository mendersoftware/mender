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
//go:build !nodbus && cgo
// +build !nodbus,cgo

// Based on: https://github.com/gotk3/gotk3/blob/v0.5.0/gio/utils.go

package dbus

// #include <glib.h>
import "C"

func goString(cstr *C.gchar) string {
	return C.GoString((*C.char)(cstr))
}

func goBool(b C.gboolean) bool {
	return b != C.FALSE
}
