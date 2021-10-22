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
	"testing"

	"github.com/mendersoftware/mender/common/dbus/test"
)

var dbusAPITest *test.DBusTestAPI

// We need to start our own DBus server to avoid the need for a session DBus
// server to already be running.
func TestMain(m *testing.M) {
	srv := test.NewDBusTestServer()
	defer srv.Close()

	dbusAPITest = srv.GetDBusAPI()

	m.Run()
}
