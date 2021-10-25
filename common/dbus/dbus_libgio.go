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

	"github.com/mendersoftware/mender/common/dbus/dbus_internal"

	"github.com/pkg/errors"

	log "github.com/sirupsen/logrus"
)

var dbusAPI = &dbusAPILibGio{
	MethodCallCallbacks: make(map[string]MethodCallCallback),
	SignalChannels: make(map[string][]signalRegistration),
}

type signalRegistration struct {
	channel SignalChannel
	id      C.guint
}

type dbusAPILibGio struct {
	MethodCallCallbacksMutex sync.Mutex
	MethodCallCallbacks      map[string]MethodCallCallback

	SignalChannelsMutex sync.Mutex
	SignalChannels      map[string][]signalRegistration
}

func gDBusConnection(ptr Handle) *C.GDBusConnection {
	return (*C.GDBusConnection)(ptr)
}

func gMainLoop(ptr MainLoop) *C.GMainLoop {
	return (*C.GMainLoop)(ptr)
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
	defer C.free(unsafe.Pointer(cstr))
	return goBool(C.g_dbus_is_guid(cstr))
}

// BusGet synchronously connects to the message bus specified by bus_type
// https://developer.gnome.org/gio/stable/GDBusConnection.html#g-bus-get-sync
func (d *dbusAPILibGio) BusGet(busType uint) (Handle, error) {
	var gerror *C.GError
	conn := C.g_bus_get_sync(C.GBusType(busType), nil, &gerror)
	if Handle(gerror) != nil {
		return Handle(nil), dbus_internal.ErrorFromNative(Handle(gerror))
	}

	// For most applications it makes sense to close when the connection to
	// the session ends. But Mender should keep running so that a broken
	// DBus setup does not prevent the device from being updated.
	C.g_dbus_connection_set_exit_on_close(conn, C.gboolean(0))

	return Handle(conn), nil
}

// BusOwnNameOnConnection starts acquiring name on the bus
// https://developer.gnome.org/gio/stable/gio-Owning-Bus-Names.html#g-bus-own-name-on-connection
func (d *dbusAPILibGio) BusOwnNameOnConnection(conn Handle, name string, flags uint) (uint, error) {
	gconn := gDBusConnection(conn)
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
func (d *dbusAPILibGio) BusUnownName(gid uint) {
	C.g_bus_unown_name(C.guint(gid))
}

// BusRegisterInterface registers an object for a given interface
// https://developer.gnome.org/gio/stable/gio-D-Bus-Introspection-Data.html#g-dbus-node-info-new-for-xml
// https://developer.gnome.org/gio/stable/GDBusConnection.html#g-dbus-connection-register-object
func (d *dbusAPILibGio) BusRegisterInterface(conn Handle, path string, interfaceXML string) (uint, error) {
	var gerror *C.GError

	// extract interface from XML using introspection
	introspection := C.CString(interfaceXML)
	defer C.free(unsafe.Pointer(introspection))
	nodeInfo := C.g_dbus_node_info_new_for_xml(introspection, &gerror)
	if Handle(gerror) != nil {
		return 0, dbus_internal.ErrorFromNative(Handle(gerror))
	}
	defer C.g_dbus_node_info_unref(nodeInfo)

	gconn := gDBusConnection(conn)
	cpath := C.CString(path)
	defer C.free(unsafe.Pointer(cpath))

	// register the interface in the bus
	// TODO Need to pass user data here.
	gid := C.g_dbus_connection_register_object(gconn, cpath, *nodeInfo.interfaces, C.get_interface_vtable(), nil, nil, &gerror)
	if Handle(gerror) != nil {
		return 0, dbus_internal.ErrorFromNative(Handle(gerror))
	} else if gid <= 0 {
		return 0, errors.New(fmt.Sprintf("failed to register the object interface (gid = %d)", gid))
	}
	return uint(gid), nil
}

// BusUnregisterInterface unregisters a previously registered interface.
// https://developer.gnome.org/gio/stable/GDBusConnection.html#g-dbus-connection-unregister-object
func (d *dbusAPILibGio) BusUnregisterInterface(conn Handle, gid uint) bool {
	gconn := gDBusConnection(conn)
	return C.g_dbus_connection_unregister_object(gconn, C.uint(gid)) != 0
}

// RegisterMethodCallCallback registers a method call callback
func (d *dbusAPILibGio) RegisterMethodCallCallback(path string, interfaceName string, method string, callback MethodCallCallback) {
	key := keyForPathInterfaceNameAndMethod(path, interfaceName, method)
	d.MethodCallCallbacksMutex.Lock()
	defer d.MethodCallCallbacksMutex.Unlock()
	d.MethodCallCallbacks[key] = callback
}

// UnregisterMethodCallCallback unregisters a method call callback
func (d *dbusAPILibGio) UnregisterMethodCallCallback(path string, interfaceName string, method string) {
	key := keyForPathInterfaceNameAndMethod(path, interfaceName, method)
	d.MethodCallCallbacksMutex.Lock()
	defer d.MethodCallCallbacksMutex.Unlock()
	delete(d.MethodCallCallbacks, key)
}

// MainLoopNew creates a new GMainLoop structure
// https://developer.gnome.org/glib/stable/glib-The-Main-Event-Loop.html#g-main-loop-new
func (d *dbusAPILibGio) MainLoopNew() MainLoop {
	loop := MainLoop(C.g_main_loop_new(nil, 0))
	runtime.SetFinalizer(&loop, func(loop *MainLoop) {
		gloop := gMainLoop(*loop)
		C.g_main_loop_unref(gloop)
	})
	return loop
}

// MainLoopRun runs a main loop until MainLoopQuit() is called
// https://developer.gnome.org/glib/stable/glib-The-Main-Event-Loop.html#g-main-loop-run
func (d *dbusAPILibGio) MainLoopRun(loop MainLoop) {
	gloop := gMainLoop(loop)
	go C.g_main_loop_run(gloop)
}

// MainLoopQuit stops a main loop from running
// https://developer.gnome.org/glib/stable/glib-The-Main-Event-Loop.html#g-main-loop-quit
func (d *dbusAPILibGio) MainLoopQuit(loop MainLoop) {
	gloop := gMainLoop(loop)
	C.g_main_loop_quit(gloop)
}

// EmitSignal emits a signal
// https://developer.gnome.org/gio/stable/GDBusConnection.html#g-dbus-connection-emit-signal
func (d *dbusAPILibGio) EmitSignal(conn Handle, destinationBusName string, objectPath string, interfaceName string, signalName string, parameters... interface{}) error {
	var gerror *C.GError
	gconn := gDBusConnection(conn)
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
	cparameters, err := interfaceListToGVariantTuple(parameters)
	if err != nil {
		return err
	}
	C.g_dbus_connection_emit_signal(gconn, cdestinationBusName, cobjectPath, cinterfaceName, csignalName, cparameters, &gerror)
	if Handle(gerror) != nil {
		dbus_internal.ErrorFromNative(Handle(gerror))
	}
	return nil
}

//export handle_method_call_callback
func handle_method_call_callback(objectPath, interfaceName, methodName *C.gchar, parameters *C.gchar) *C.GVariant {
	goObjectPath := C.GoString(objectPath)
	goInterfaceName := C.GoString(interfaceName)
	goMethodName := C.GoString(methodName)
	goParameters := C.GoString(parameters)
	key := keyForPathInterfaceNameAndMethod(goObjectPath, goInterfaceName, goMethodName)

	dbusAPI.MethodCallCallbacksMutex.Lock()
	callback, ok := dbusAPI.MethodCallCallbacks[key]
	dbusAPI.MethodCallCallbacksMutex.Unlock()
	if !ok {
		log.Errorf("No dbus callback set for this key: %s", key)
		return nil
	}

	result, err := callback(goObjectPath, goInterfaceName, goMethodName, goParameters)
	if err != nil {
		log.Errorf("handle_method_call_callback: Callback returned an error: %s", err)
		return nil
	}
	gVar, err := interfaceListToGVariantTuple(result)
	if err != nil {
		log.Error(err.Error())
		return nil
	}
	return gVar
}

func keyForPathInterfaceNameAndMethod(path string, interfaceName string, method string) string {
	return path + "/" + interfaceName + "." + method
}

func (d *dbusAPILibGio) RegisterSignalChannel(conn Handle, busName, objectPath, interfaceName, methodName string, ch SignalChannel) {
	cBusName := C.CString(busName)
	defer C.free(unsafe.Pointer(cBusName))
	cObjectPath := C.CString(objectPath)
	defer C.free(unsafe.Pointer(cObjectPath))
	cInterfaceName := C.CString(interfaceName)
	defer C.free(unsafe.Pointer(cInterfaceName))
	cMethodName := C.CString(methodName)
	defer C.free(unsafe.Pointer(cMethodName))

	gconn := gDBusConnection(conn)

	d.SignalChannelsMutex.Lock()
	defer d.SignalChannelsMutex.Unlock()

	id := C.g_dbus_connection_signal_subscribe(
		gconn,
		cBusName,
		cInterfaceName,
		cMethodName,
		cObjectPath,
		nil, // arg0
		0, // flags
		(*[0]byte)(C.dbusSignalCallback),
		nil, // user_data,
		nil, // user_data_free_func,
	)

	d.SignalChannels[methodName] = append(d.SignalChannels[methodName], signalRegistration{
		ch,
		id,
	})
}

func (d *dbusAPILibGio) UnregisterSignalChannel(conn Handle, methodName string, ch SignalChannel) {
	d.SignalChannelsMutex.Lock()
	defer d.SignalChannelsMutex.Unlock()

	list := d.SignalChannels[methodName]
	if list == nil {
		return
	}

	gconn := gDBusConnection(conn)

	var newlist []signalRegistration
	for _, entry := range list {
		if entry.channel == ch {
			C.g_dbus_connection_signal_unsubscribe(gconn, entry.id)
		} else {
			newlist = append(newlist, entry)
		}
	}

	d.SignalChannels[methodName] = newlist
}

//export dbusSignalCallback
func dbusSignalCallback(
	connection *C.GDBusConnection,
	senderName *C.gchar,
	objectPath *C.gchar,
	interfaceName *C.gchar,
	signalName *C.gchar,
	parameters *C.GVariant,
	userData C.gpointer) {

	goSignalName := C.GoString(signalName)
	goObjectPath := C.GoString(objectPath)
	goInterfaceName := C.GoString(interfaceName)

	log.Debugf("Received D-Bus signal %s (objectPath=%s, interfaceName=%s)",
		goSignalName,
		goObjectPath,
		goInterfaceName)

	dbusAPI.SignalChannelsMutex.Lock()
	defer dbusAPI.SignalChannelsMutex.Unlock()

	signalChannels := dbusAPI.SignalChannels[C.GoString(signalName)]
	if signalChannels == nil {
		return
	}

	goParams, err := gVariantTuple2InterfaceList(parameters)
	if err != nil {
		log.Warnf("Received D-Bus signal %s (objectPath=%s, interfaceName=%s) which failed parameter parsing: %s",
			goSignalName,
			goObjectPath,
			goInterfaceName,
			err.Error())
		return
	}

	for _, sChannel := range signalChannels {
		// Non-blocking write.
		select {
		case sChannel.channel <- goParams:
			// OK! Nothing more to do.
		default:
			log.Warnf("D-Bus signal %s (objectPath=%s, interfaceName=%s) was dropped (channel full)",
				goSignalName,
				goObjectPath,
				goInterfaceName)
		}
	}
}

// Call DBus endpoint with no parameters.
func (d *dbusAPILibGio) Call0(conn Handle, busName, objectPath, interfaceName, methodName string) ([]interface{}, error) {
	cBusName := C.CString(busName)
	defer C.free(unsafe.Pointer(cBusName))
	cObjectPath := C.CString(objectPath)
	defer C.free(unsafe.Pointer(cObjectPath))
	cInterfaceName := C.CString(interfaceName)
	defer C.free(unsafe.Pointer(cInterfaceName))
	cMethodName := C.CString(methodName)
	defer C.free(unsafe.Pointer(cMethodName))

	log.Debugf("Calling D-Bus method %s (busName=%s, objectPath=%s, interfaceName=%s)",
		methodName,
		busName,
		objectPath,
		interfaceName)

	gconn := gDBusConnection(conn)

	var gerror *C.GError
	gResult := C.g_dbus_connection_call_sync(
		gconn,
		cBusName,
		cObjectPath,
		cInterfaceName,
		cMethodName,
		nil, // parameters
		nil, //replyType
		0, // flags
		-1, // timeout, -1 == default
		nil, // cancellable
		&gerror,
	)
	if Handle(gerror) != nil {
		return nil, dbus_internal.ErrorFromNative(Handle(gerror))
	}

	defer C.g_variant_unref(gResult)

	return gVariantTuple2InterfaceList(gResult)
}

func gVariantTuple2InterfaceList(v *C.GVariant) ([]interface{}, error) {
	if v == nil {
		return nil, errors.New("gVariantTuple2InterfaceList called with NULL GVariant. Should not happen")
	}

	if C.g_variant_is_of_type(v, C.G_VARIANT_TYPE_TUPLE) == 0 {
		typeStr := C.g_variant_print(v, 1)
		defer C.g_free(C.gpointer(unsafe.Pointer(typeStr)))
		return nil, errors.Errorf("Unsupported DBus result type, must be a tuple: %s", C.GoString(typeStr))
	}

	children := C.g_variant_n_children(v)

	result := make([]interface{}, 0, children)
	for i := C.ulong(0); i < children; i++ {
		value := C.g_variant_get_child_value(v, i)

		switch rune(*C.g_variant_get_type_string(value)) {
		case 'b':
			result = append(result, C.g_variant_get_boolean(value) != 0)
		case 'i':
			result = append(result, int(C.g_variant_get_int32(value)))
		case 's':
			result = append(result, C.GoString(C.g_variant_get_string(value, nil)))

		default:
			// Should be easy to add more here, this is a good resource:
			// https://docs.gtk.org/glib/struct.VariantType.html#gvariant-type-strings

			defer C.g_variant_unref(value)
			return nil, errors.Errorf("Unsupported DBus result type: %s",
				C.GoString(C.g_variant_get_type_string(value)))
		}
		C.g_variant_unref(value)
	}

	return result, nil
}

func interfaceListToGVariantTuple(list []interface{}) (*C.GVariant, error) {
	gVarList := make([]*C.GVariant, len(list))

	for i, entry := range list {
		switch e := entry.(type) {
		case string:
			gVarList[i] = C.g_variant_new_take_string(C.CString(e))
		case int:
			gVarList[i] = C.g_variant_new_int32(C.gint32(e))
		case bool:
			var vbool C.gboolean
			if e {
				vbool = 1
			} else {
				vbool = 0
			}
			gVarList[i] = C.g_variant_new_boolean(vbool)
		default:
			for _, gVariant := range gVarList {
				if gVariant != nil {
					C.g_object_unref(C.gpointer(unsafe.Pointer(gVariant)))
				}
			}
			return nil, errors.Errorf("Unsupported DBus encoding type: %T", entry)
		}
	}

	return C.g_variant_new_tuple(&gVarList[0], C.ulong(len(gVarList))), nil
}
