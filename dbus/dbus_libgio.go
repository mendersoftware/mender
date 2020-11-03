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
// #include "dbus_libgio.go.h"
import "C"
import (
	"fmt"
	"unsafe"

	"github.com/pkg/errors"
)

type dbusAPILibGio struct {
	MethodCallCallbacks map[string]MethodCallCallback
}

// GenerateGUID generates a D-Bus GUID that can be used with e.g. g_dbus_connection_new().
// https://developer.gnome.org/gio/stable/gio-D-Bus-Utilities.html#g-dbus-generate-guid
func (d *dbusAPILibGio) GenerateGUID() string {
	guid := C.g_dbus_generate_guid()
	defer C.g_free(C.gpointer(guid))
	return goString(guid)
}

// IsGUID checks if string is a D-Bus GUID.
// https://developer.gnome.org/gio/stable/gio-D-Bus-Utilities.html#g-dbus-is-guid
func (d *dbusAPILibGio) IsGUID(str string) bool {
	cstr := C.CString(str)
	return goBool(C.g_dbus_is_guid(cstr))
}

// BusGet synchronously connects to the message bus specified by bus_type
// https://developer.gnome.org/gio/stable/GDBusConnection.html#g-bus-get-sync
func (d *dbusAPILibGio) BusGet(busType uint) (Handle, error) {
	var gerror *C.GError
	conn := C.g_bus_get_sync(C.GBusType(busType), nil, &gerror)
	if Handle(gerror) != nil {
		return Handle(nil), ErrorFromNative(Handle(gerror))
	}
	return Handle(conn), nil
}

// BusOwnNameOnConnection starts acquiring name on the bus
// https://developer.gnome.org/gio/stable/gio-Owning-Bus-Names.html#g-bus-own-name-on-connection
func (d *dbusAPILibGio) BusOwnNameOnConnection(conn Handle, name string, flags uint) (uint, error) {
	gconn := C.to_gdbusconnection(unsafe.Pointer(conn))
	cname := C.CString(name)
	cflags := C.GBusNameOwnerFlags(flags)
	gid := C.g_bus_own_name_on_connection(gconn, cname, cflags, nil, nil, nil, nil)
	if gid <= 0 {
		return 0, errors.New(fmt.Sprintf("failed to own name on bus (gid = %d)", gid))
	}
	return uint(gid), nil
}

// BusRegisterObjectForInterface registers an object for a given interface
// https://developer.gnome.org/gio/stable/gio-D-Bus-Introspection-Data.html#g-dbus-node-info-new-for-xml
// https://developer.gnome.org/gio/stable/GDBusConnection.html#g-dbus-connection-register-object
func (d *dbusAPILibGio) BusRegisterInterface(conn Handle, path string, interfaceXML string) (uint, error) {
	var gerror *C.GError
	// extract interface from XML using introspection
	introspection := C.CString(interfaceXML)
	nodeInfo := C.g_dbus_node_info_new_for_xml(introspection, &gerror)
	if Handle(gerror) != nil {
		return 0, ErrorFromNative(Handle(gerror))
	}
	// register the interface in the bus
	gconn := C.to_gdbusconnection(unsafe.Pointer(conn))
	cpath := C.CString(path)
	gid := C.g_dbus_connection_register_object(gconn, cpath, *nodeInfo.interfaces, C.get_interface_vtable(), nil, nil, &gerror)
	if Handle(gerror) != nil {
		return 0, ErrorFromNative(Handle(gerror))
	} else if gid <= 0 {
		return 0, errors.New(fmt.Sprintf("failed to register the object interface (gid = %d)", gid))
	}
	return uint(gid), nil
}

// RegisterMethodCallCallback registers a method call callback
func (d *dbusAPILibGio) RegisterMethodCallCallback(path string, interfaceName string, method string, callback MethodCallCallback) {
	key := keyForPathInterfaceNameAndMethod(path, interfaceName, method)
	d.MethodCallCallbacks[key] = callback
}

// MainLoopNew creates a new GMainLoop structure
// https://developer.gnome.org/glib/stable/glib-The-Main-Event-Loop.html#g-main-loop-new
func (d *dbusAPILibGio) MainLoopNew() Handle {
	return Handle(C.g_main_loop_new(nil, 0))
}

// MainLoopRun runs a main loop until MainLoopQuit() is called
// https://developer.gnome.org/glib/stable/glib-The-Main-Event-Loop.html#g-main-loop-run
func (d *dbusAPILibGio) MainLoopRun(loop Handle) {
	gloop := C.to_gmainloop(unsafe.Pointer(loop))
	go C.g_main_loop_run(gloop)
}

// MainLoopQuit stops a main loop from running
// https://developer.gnome.org/glib/stable/glib-The-Main-Event-Loop.html#g-main-loop-quit
func (d *dbusAPILibGio) MainLoopQuit(loop Handle) {
	gloop := C.to_gmainloop(unsafe.Pointer(loop))
	C.g_main_loop_quit(gloop)
}

// EmitSignal emits a signal
// https://developer.gnome.org/gio/stable/GDBusConnection.html#g-dbus-connection-emit-signal
func (d *dbusAPILibGio) EmitSignal(conn Handle, destinationBusName string, objectPath string, interfaceName string, signalName string) error {
	var gerror *C.GError
	gconn := C.to_gdbusconnection(unsafe.Pointer(conn))
	var cdestinationBusName *C.gchar
	if destinationBusName != "" {
		cdestinationBusName = C.CString(destinationBusName)
	} else {
		cdestinationBusName = nil
	}
	cobjectPath := C.CString(objectPath)
	cinterfaceName := C.CString(interfaceName)
	csignalName := C.CString(signalName)
	C.g_dbus_connection_emit_signal(gconn, cdestinationBusName, cobjectPath, cinterfaceName, csignalName, nil, &gerror)
	if Handle(gerror) != nil {
		ErrorFromNative(Handle(gerror))
	}
	return nil
}

//export handle_method_call_callback
func handle_method_call_callback(objectPath, interfaceName, methodName *C.gchar) *C.GVariant {
	goObjectPath := C.GoString(objectPath)
	goInterfaceName := C.GoString(interfaceName)
	goMethodName := C.GoString(methodName)
	key := keyForPathInterfaceNameAndMethod(goObjectPath, goInterfaceName, goMethodName)
	api, _ := GetDBusAPI()
	if callback, ok := api.(*dbusAPILibGio).MethodCallCallbacks[key]; ok {
		result, err := callback(goObjectPath, goInterfaceName, goMethodName)
		if err != nil {
			return nil
		} else if v, ok := result.(string); ok {
			return C.g_variant_new_from_string((*C.gchar)(C.CString(v)))
		} else if v, ok := result.(bool); ok {
			var vbool C.gboolean
			if v {
				vbool = 1
			} else {
				vbool = 0
			}
			return C.g_variant_new_from_boolean(vbool)
		}
	}
	return nil
}

func keyForPathInterfaceNameAndMethod(path string, interfaceName string, method string) string {
	return path + "/" + interfaceName + "." + method
}

func init() {
	dbusAPI = &dbusAPILibGio{
		MethodCallCallbacks: make(map[string]MethodCallCallback),
	}
}
