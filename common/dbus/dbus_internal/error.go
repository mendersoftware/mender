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

package dbus_internal

// #cgo pkg-config: gio-2.0
// #include <gio/gio.h>
// #include <glib/gerror.h>
import "C"
import "unsafe"

// Error is a struct which holds a C error
type Error struct {
	GError *C.GError
}

func (v *Error) Error() string {
	return v.Message()
}

// Message returns the error message
func (v *Error) Message() string {
	if Handle(v.GError) == nil || Handle(v.GError.message) == nil {
		return ""
	}
	return C.GoString(v.GError.message)
}

// ErrorFromNative returns an Error object from a native error
func ErrorFromNative(err Handle) error {
	return &Error{(*C.GError)(unsafe.Pointer(err))}
}

// errorToNative returns an Error object from a native error
func errorToNative(err error) Handle {
	errMessage := C.CString(err.Error())
	gErr := C.GError{}
	gErr.message = errMessage
	return Handle(&gErr)
}
