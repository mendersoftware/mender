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

package dbus

// #cgo pkg-config: gio-2.0
// #include <stdlib.h>
// #include <stdio.h>
// #include <gio/gio.h>
// #include "dbus_libgio.go.h"
import "C"
import (
	"fmt"
	"runtime"
	"sync"
	"unsafe"

	"github.com/pkg/errors"

	log "github.com/sirupsen/logrus"
)

func init() {
	dbusAPI = NewDBusAPI()
}

var dbusAPIRegisteredObjectsMutex sync.Mutex
var dbusAPIRegisteredObjects = struct {
	cToGo map[C.gpointer]*dbusAPILibGioInner
	goToC map[*dbusAPILibGioInner]C.gpointer
}{
	make(map[C.gpointer]*dbusAPILibGioInner),
	make(map[*dbusAPILibGioInner]C.gpointer),
}
var dbusAPIRegisteredObjectsCounter uintptr = 1

type dbusAPILibGio struct {
	// We need this inner type to be able to set a finalizer on the outer
	// type.
	*dbusAPILibGioInner
}

type dbusAPILibGioInner struct {
	MethodCallCallbacksMutex sync.Mutex
	MethodCallCallbacks      map[string]MethodCallCallback
}

func NewDBusAPI() DBusAPI {
	d := &dbusAPILibGio{
		&dbusAPILibGioInner{
			MethodCallCallbacks: make(map[string]MethodCallCallback),
		},
	}

	// We need to jump through some hoops in the integration with libgio. We
	// have to register the DBusAPI object and let libgio keep a pointer to
	// it. However, this is not allowed by the CGO rules. Particular section
	// from the docs:
	//
	// "C code may not keep a copy of a Go pointer after the call returns."
	//
	// Presumably, this is because the garbage collector will lose track of
	// it, and can't update it as part of garbage collection / memory
	// restructuring.
	//
	// So instead, we use a fake C pointer, which is actually just a unique
	// int value, and pass this to libgio. At the same time we store this
	// value in a map on the Go side, and use it to recover the Go pointer
	// later when we get the pointer back from libgio.
	//
	// Since we are not actually allocating any memory, we don't need to
	// free the C pointer.
	dbusAPIRegisteredObjectsMutex.Lock()
	defer dbusAPIRegisteredObjectsMutex.Unlock()
	cPointer := C.gpointer(unsafe.Pointer(dbusAPIRegisteredObjectsCounter))
	// Monotonically increasing fake memory address, IOW unique.
	dbusAPIRegisteredObjectsCounter++
	dbusAPIRegisteredObjects.cToGo[cPointer] = d.dbusAPILibGioInner
	dbusAPIRegisteredObjects.goToC[d.dbusAPILibGioInner] = cPointer

	runtime.SetFinalizer(d, func(d *dbusAPILibGio) {
		dbusAPIRegisteredObjectsMutex.Lock()
		defer dbusAPIRegisteredObjectsMutex.Unlock()
		cPointer := dbusAPIRegisteredObjects.goToC[d.dbusAPILibGioInner]
		// Clear object mapping when Go object is garbage collected.
		delete(dbusAPIRegisteredObjects.cToGo, cPointer)
		delete(dbusAPIRegisteredObjects.goToC, d.dbusAPILibGioInner)
	})

	return d
}

// GenerateGUID generates a D-Bus GUID that can be used with e.g. g_dbus_connection_new().
// https://developer.gnome.org/gio/stable/gio-D-Bus-Utilities.html#g-dbus-generate-guid
func (d *dbusAPILibGioInner) GenerateGUID() string {
	guid := C.g_dbus_generate_guid()
	defer C.g_free(C.gpointer(guid))
	return goString(guid)
}

// IsGUID checks if string is a D-Bus GUID.
// https://developer.gnome.org/gio/stable/gio-D-Bus-Utilities.html#g-dbus-is-guid
func (d *dbusAPILibGioInner) IsGUID(str string) bool {
	cstr := C.CString(str)
	defer C.free(unsafe.Pointer(cstr))
	return goBool(C.g_dbus_is_guid(cstr))
}

// BusGet synchronously connects to the message bus specified by bus_type
// https://developer.gnome.org/gio/stable/GDBusConnection.html#g-bus-get-sync
func (d *dbusAPILibGioInner) BusGet(busType uint) (Handle, error) {
	var gerror *C.GError
	conn := C.g_bus_get_sync(C.GBusType(busType), nil, &gerror)
	if Handle(gerror) != nil {
		return Handle(nil), ErrorFromNative(Handle(gerror))
	}

	// For most applications it makes sense to close when the connection to
	// the session ends. But Mender should keep running so that a broken
	// DBus setup does not prevent the device from being updated.
	C.g_dbus_connection_set_exit_on_close(conn, C.gboolean(0))

	return Handle(conn), nil
}

// BusOwnNameOnConnection starts acquiring name on the bus
// https://developer.gnome.org/gio/stable/gio-Owning-Bus-Names.html#g-bus-own-name-on-connection
func (d *dbusAPILibGioInner) BusOwnNameOnConnection(conn Handle, name string, flags uint) (uint, error) {
	gconn := C.to_gdbusconnection(unsafe.Pointer(conn))
	cname := C.CString(name)
	defer C.free(unsafe.Pointer(cname))
	cflags := C.GBusNameOwnerFlags(flags)
	gid := C.g_bus_own_name_on_connection(gconn, cname, cflags, nil, nil, nil, nil)
	if gid <= 0 {
		return 0, errors.New(fmt.Sprintf("failed to own name on bus (gid = %d)", gid))
	}
	return uint(gid), nil
}

// BusUnownName releases name on the bus
// https://developer.gnome.org/gio/stable/gio-Owning-Bus-Names.html#g-bus-unown-name
func (d *dbusAPILibGioInner) BusUnownName(gid uint) {
	C.g_bus_unown_name(C.guint(gid))
}

// BusRegisterInterface registers an object for a given interface
// https://developer.gnome.org/gio/stable/gio-D-Bus-Introspection-Data.html#g-dbus-node-info-new-for-xml
// https://developer.gnome.org/gio/stable/GDBusConnection.html#g-dbus-connection-register-object
func (d *dbusAPILibGioInner) BusRegisterInterface(
	conn Handle,
	path string,
	interfaceXML string,
) (uint, error) {
	var gerror *C.GError

	// extract interface from XML using introspection
	introspection := C.CString(interfaceXML)
	defer C.free(unsafe.Pointer(introspection))
	nodeInfo := C.g_dbus_node_info_new_for_xml(introspection, &gerror)
	if Handle(gerror) != nil {
		return 0, ErrorFromNative(Handle(gerror))
	}
	defer C.g_dbus_node_info_unref(nodeInfo)

	gconn := C.to_gdbusconnection(unsafe.Pointer(conn))
	cpath := C.CString(path)
	defer C.free(unsafe.Pointer(cpath))

	dbusAPIRegisteredObjectsMutex.Lock()
	cPointer := dbusAPIRegisteredObjects.goToC[d]
	dbusAPIRegisteredObjectsMutex.Unlock()

	// register the interface in the bus
	gid := C.g_dbus_connection_register_object(
		gconn,
		cpath,
		*nodeInfo.interfaces,
		C.get_interface_vtable(),
		cPointer,
		nil,
		&gerror,
	)
	if Handle(gerror) != nil {
		return 0, ErrorFromNative(Handle(gerror))
	} else if gid <= 0 {
		return 0, errors.New(fmt.Sprintf("failed to register the object interface (gid = %d)", gid))
	}
	return uint(gid), nil
}

// BusUnregisterInterface unregisters a previously registered interface.
// https://developer.gnome.org/gio/stable/GDBusConnection.html#g-dbus-connection-unregister-object
func (d *dbusAPILibGioInner) BusUnregisterInterface(conn Handle, gid uint) bool {
	gconn := C.to_gdbusconnection(unsafe.Pointer(conn))
	return C.g_dbus_connection_unregister_object(gconn, C.uint(gid)) != 0
}

// RegisterMethodCallCallback registers a method call callback
func (d *dbusAPILibGioInner) RegisterMethodCallCallback(
	path string,
	interfaceName string,
	method string,
	callback MethodCallCallback,
) {
	key := keyForPathInterfaceNameAndMethod(path, interfaceName, method)
	d.MethodCallCallbacksMutex.Lock()
	defer d.MethodCallCallbacksMutex.Unlock()
	d.MethodCallCallbacks[key] = callback
}

// UnregisterMethodCallCallback unregisters a method call callback
func (d *dbusAPILibGioInner) UnregisterMethodCallCallback(
	path string,
	interfaceName string,
	method string,
) {
	key := keyForPathInterfaceNameAndMethod(path, interfaceName, method)
	d.MethodCallCallbacksMutex.Lock()
	defer d.MethodCallCallbacksMutex.Unlock()
	delete(d.MethodCallCallbacks, key)
}

// MainLoopNew creates a new GMainLoop structure
// https://developer.gnome.org/glib/stable/glib-The-Main-Event-Loop.html#g-main-loop-new
func (d *dbusAPILibGioInner) MainLoopNew() MainLoop {
	loop := MainLoop(C.g_main_loop_new(nil, 0))
	runtime.SetFinalizer(&loop, func(loop *MainLoop) {
		gloop := C.to_gmainloop(unsafe.Pointer(*loop))
		C.g_main_loop_unref(gloop)
	})
	return loop
}

// MainLoopRun runs a main loop until MainLoopQuit() is called
// https://developer.gnome.org/glib/stable/glib-The-Main-Event-Loop.html#g-main-loop-run
func (d *dbusAPILibGioInner) MainLoopRun(loop MainLoop) {
	gloop := C.to_gmainloop(unsafe.Pointer(loop))
	go C.g_main_loop_run(gloop)
}

// MainLoopQuit stops a main loop from running
// https://developer.gnome.org/glib/stable/glib-The-Main-Event-Loop.html#g-main-loop-quit
func (d *dbusAPILibGioInner) MainLoopQuit(loop MainLoop) {
	gloop := C.to_gmainloop(unsafe.Pointer(loop))
	C.g_main_loop_quit(gloop)
}

// EmitSignal emits a signal
// https://developer.gnome.org/gio/stable/GDBusConnection.html#g-dbus-connection-emit-signal
func (d *dbusAPILibGioInner) EmitSignal(
	conn Handle,
	destinationBusName string,
	objectPath string,
	interfaceName string,
	signalName string,
	parameters interface{},
) error {
	var gerror *C.GError
	gconn := C.to_gdbusconnection(unsafe.Pointer(conn))
	var cdestinationBusName *C.gchar
	if destinationBusName != "" {
		cdestinationBusName = C.CString(destinationBusName)
		defer C.free(unsafe.Pointer(cdestinationBusName))
	} else {
		cdestinationBusName = nil
	}
	cobjectPath := C.CString(objectPath)
	defer C.free(unsafe.Pointer(cobjectPath))
	cinterfaceName := C.CString(interfaceName)
	defer C.free(unsafe.Pointer(cinterfaceName))
	csignalName := C.CString(signalName)
	defer C.free(unsafe.Pointer(csignalName))
	cparameters := interfaceToGVariant(parameters)
	C.g_dbus_connection_emit_signal(
		gconn,
		cdestinationBusName,
		cobjectPath,
		cinterfaceName,
		csignalName,
		cparameters,
		&gerror,
	)
	if Handle(gerror) != nil {
		return ErrorFromNative(Handle(gerror))
	}
	return nil
}

func interfaceToGVariant(result interface{}) *C.GVariant {
	if v, ok := result.(TokenAndServerURL); ok {
		strToken := C.CString(v.Token)
		strServerURL := C.CString(v.ServerURL)
		defer C.free(unsafe.Pointer(strToken))
		defer C.free(unsafe.Pointer(strServerURL))
		return C.g_variant_new_from_two_strings((*C.gchar)(strToken), (*C.gchar)(strServerURL))
	} else if v, ok := result.(string); ok {
		str := C.CString(v)
		defer C.free(unsafe.Pointer(str))
		return C.g_variant_new_from_string((*C.gchar)(str))
	} else if v, ok := result.(bool); ok {
		var vbool C.gboolean
		if v {
			vbool = 1
		} else {
			vbool = 0
		}
		return C.g_variant_new_from_boolean(vbool)
	} else if v, ok := result.(int); ok {
		return C.g_variant_new_from_int(C.gint(v))
	} else {
		log.Errorf("Failed to encode the type (%T) to send it on the D-Bus", result)
	}
	return nil
}

//export handle_method_call_callback
func handle_method_call_callback(
	objectPath, interfaceName, methodName *C.gchar,
	parameters *C.gchar,
	userData C.gpointer,
) *C.GVariant {
	goObjectPath := C.GoString(objectPath)
	goInterfaceName := C.GoString(interfaceName)
	goMethodName := C.GoString(methodName)
	goParameters := C.GoString(parameters)
	key := keyForPathInterfaceNameAndMethod(goObjectPath, goInterfaceName, goMethodName)

	dbusAPIRegisteredObjectsMutex.Lock()
	d := dbusAPIRegisteredObjects.cToGo[userData]
	dbusAPIRegisteredObjectsMutex.Unlock()

	d.MethodCallCallbacksMutex.Lock()
	callback, ok := d.MethodCallCallbacks[key]
	d.MethodCallCallbacksMutex.Unlock()
	if ok {
		result, err := callback(goObjectPath, goInterfaceName, goMethodName, goParameters)
		if err != nil {
			log.Errorf("handle_method_call_callback: Callback returned an error: %s", err)
			return nil
		}
		return interfaceToGVariant(result)
	}
	return nil
}

func keyForPathInterfaceNameAndMethod(path string, interfaceName string, method string) string {
	return path + "/" + interfaceName + "." + method
}
