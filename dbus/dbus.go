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

package dbus

import "github.com/pkg/errors"

var dbusAPI DBusAPI = nil

// DBusAPI is the interface which describes a DBus API
type DBusAPI interface {
	// GenerateGUID generates a D-Bus GUID that can be used with e.g. g_dbus_connection_new()
	GenerateGUID() string
	// IsGUID checks if string is a D-Bus GUID.
	IsGUID(string) bool
	// BusGet synchronously connects to the message bus specified by bus_type
	BusGet(uint) (Pointer, error)
	// BusOwnNameOnConnection starts acquiring name on the bus
	BusOwnNameOnConnection(Pointer, string, uint) (uint, error)
	// BusRegisterInterface registers an object for a given interface
	BusRegisterInterface(Pointer, string, string) (uint, error)
	// RegisterMethodCallCallback registers a method call callback
	RegisterMethodCallCallback(string, string, string, MethodCallCallback)
	// MainLoopNew creates a new GMainLoop structure
	MainLoopNew() Pointer
	// MainLoopRun runs a main loop until MainLoopQuit() is called
	MainLoopRun(Pointer)
	// MainLoopQuit stops a main loop from running
	MainLoopQuit(Pointer)
}

// MethodCallCallback represents a method_call callback
type MethodCallCallback = func(objectPath string, interfaceName string, methodName string) (interface{}, error)

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
