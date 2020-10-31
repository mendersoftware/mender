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

	// IsGUID Checks if string is a D-Bus GUID.
	IsGUID(string) bool
}

// GetDBusAPI returns the global DBusAPI object
func GetDBusAPI() (DBusAPI, error) {
	if dbusAPI != nil {
		return dbusAPI, nil
	}
	return nil, errors.New("no D-Bus interface available")
}
