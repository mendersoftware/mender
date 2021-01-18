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

import (
	"unsafe"

	"github.com/pkg/errors"
)

// These are unsafe pointers, only prettier :)
type Handle unsafe.Pointer
type MainLoop unsafe.Pointer

var dbusAPI DBusAPI = nil

// DBusAPI is the interface which describes a DBus API
type DBusAPI interface {
	// GenerateGUID generates a D-Bus GUID that can be used with e.g. g_dbus_connection_new()
	GenerateGUID() string
	// IsGUID checks if string is a D-Bus GUID.
	IsGUID(string) bool
	// BusGet synchronously connects to the message bus specified by bus_type
	BusGet(uint) (Handle, error)
	// BusOwnNameOnConnection starts acquiring name on the bus
	BusOwnNameOnConnection(Handle, string, uint) (uint, error)
	// BusUnownName releases name on the bus
	BusUnownName(uint)
	// BusRegisterInterface registers an object for a given interface
	BusRegisterInterface(Handle, string, string) (uint, error)
	// BusUnregisterInterface unregisters a previously registered interface.
	BusUnregisterInterface(Handle, uint) bool
	// RegisterMethodCallCallback registers a method call callback
	RegisterMethodCallCallback(string, string, string, MethodCallCallback)
	// UnregisterMethodCallCallback unregisters a method call callback
	UnregisterMethodCallCallback(string, string, string)
	// MainLoopNew creates a new GMainLoop structure
	MainLoopNew() MainLoop
	// MainLoopRun runs a main loop until MainLoopQuit() is called
	MainLoopRun(MainLoop)
	// MainLoopQuit stops a main loop from running
	MainLoopQuit(MainLoop)
	// EmitSignal emits a signal
	EmitSignal(Handle, string, string, string, string, interface{}) error
}

// MethodCallCallback represents a method_call callback
type MethodCallCallback = func(objectPath string, interfaceName string, methodName string) (interface{}, error)

// TokenAndServerURL stores values for the JWT token and the server URL
type TokenAndServerURL struct {
	Token     string
	ServerURL string
}

// GetDBusAPI returns the global DBusAPI object
func GetDBusAPI() (DBusAPI, error) {
	if dbusAPI != nil {
		return dbusAPI, nil
	}
	return nil, errors.New("no D-Bus interface available")
}

func setDBusAPI(api DBusAPI) {
	dbusAPI = api
}
