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

import (
	"fmt"
	"os"
	"testing"
	"time"

	godbus "github.com/godbus/dbus"
	"github.com/stretchr/testify/assert"
)

var libgio *dbusAPILibGio

func TestMain(m *testing.M) {
	libgio = &dbusAPILibGio{
		MethodCallCallbacks: make(map[string]MethodCallCallback),
	}
	setDBusAPI(libgio)
	exitVal := m.Run()
	os.Exit(exitVal)
}

func TestGenerateGUID(t *testing.T) {
	guid := libgio.GenerateGUID()
	assert.NotEmpty(t, guid)
}

func TestIsGUID(t *testing.T) {
	// Dummy GUID
	assert.False(t, libgio.IsGUID("fake-guid"))

	// Get and check a valid GUID
	guid := libgio.GenerateGUID()
	assert.True(t, libgio.IsGUID(guid))
}

func TestBusGet(t *testing.T) {
	conn, err := libgio.BusGet(GBusTypeSystem)
	assert.NoError(t, err)
	assert.NotNil(t, conn)
}

func TestBusOwnNameOnConnection(t *testing.T) {
	conn, err := libgio.BusGet(GBusTypeSystem)
	assert.NoError(t, err)
	assert.NotNil(t, conn)

	gid, err := libgio.BusOwnNameOnConnection(conn, "io.mender.AuthenticationManager", DBusNameOwnerFlagsNone)
	assert.NoError(t, err)
	assert.Greater(t, gid, uint(0))
}

func TestBusRegisterInterface(t *testing.T) {
	testCases := map[string]struct {
		xml  string
		path string
		err  bool
	}{
		"ok": {
			xml: `<node>
			<interface name="io.mender.Authentication1">
				<method name="GetJwtToken">
					<arg type="s" name="token" direction="out"/>
				</method>
				<method name="FetchJwtToken">
					<arg type="b" name="success" direction="out"/>
				</method>
			</interface>
		</node>`,
			path: "/io/mender/AuthenticationManager/TestBusRegisterInterface1",
			err:  false,
		},
		"ko, invalid interface": {
			xml:  "dummy-interface",
			path: "/io/mender/AuthenticationManager/TestBusRegisterInterface2",
			err:  true,
		},
		"ko, invalid path": {
			xml: `<node>
			<interface name="io.mender.Authentication1">
				<method name="GetJwtToken">
					<arg type="s" name="token" direction="out"/>
				</method>
				<method name="FetchJwtToken">
					<arg type="b" name="success" direction="out"/>
				</method>
			</interface>
		</node>`,
			path: "io/mender/AuthenticationManager/TestBusRegisterInterface3",
			err:  true,
		},
	}
	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			conn, err := libgio.BusGet(GBusTypeSystem)
			assert.NoError(t, err)
			assert.NotNil(t, conn)

			gid, err := libgio.BusOwnNameOnConnection(conn, "io.mender.AuthenticationManager", DBusNameOwnerFlagsNone)
			assert.NoError(t, err)
			assert.Greater(t, gid, uint(0))

			gid, err = libgio.BusRegisterInterface(conn, tc.path, tc.xml)
			if tc.err {
				assert.Error(t, err)
				assert.Equal(t, gid, uint(0))
			} else {
				assert.NoError(t, err)
				assert.Greater(t, gid, uint(0))
			}
		})
	}
}

func TestRegisterMethodCallCallback(t *testing.T) {
	callback := func(objectPath string, interfaceName string, methodName string) (interface{}, error) {
		return "value", nil
	}

	path := "/io/mender/AuthenticationManager"
	interfaceName := "io.mender.Authentication1"
	methodName := "GetJwtToken"
	libgio.RegisterMethodCallCallback(path, interfaceName, methodName, callback)

	key := keyForPathInterfaceNameAndMethod(path, interfaceName, methodName)
	_, ok := libgio.MethodCallCallbacks[key]
	assert.True(t, ok)

	key = keyForPathInterfaceNameAndMethod(path, interfaceName, "dummyMethod")
	_, ok = libgio.MethodCallCallbacks[key]
	assert.False(t, ok)
}

func TestHandleMethodCallCallback(t *testing.T) {
	conn, err := libgio.BusGet(GBusTypeSystem)
	assert.NoError(t, err)
	assert.NotNil(t, conn)

	const objectName = "io.mender.AuthenticationManager"

	gid, err := libgio.BusOwnNameOnConnection(conn, objectName,
		DBusNameOwnerFlagsAllowReplacement|DBusNameOwnerFlagsReplace)
	assert.NoError(t, err)
	assert.Greater(t, gid, uint(0))

	godbusConn, err := godbus.SystemBus()
	assert.NoError(t, err)
	defer godbusConn.Close()

	xml := `<node>
	<interface name="io.mender.Authentication1">
		<method name="GetJwtToken">
			<arg type="s" name="token" direction="out"/>
		</method>
		<method name="FetchJwtToken">
			<arg type="b" name="success" direction="out"/>
		</method>
	</interface>
</node>`

	testCases := map[string]struct {
		xml           string
		path          string
		interfaceName string
		methodName    string
		callback      MethodCallCallback
		outString     string
		outBoolean    bool
	}{
		"ok, string value": {
			xml:           xml,
			path:          "/io/mender/AuthenticationManager/TestHandleMethodCallCallback1",
			interfaceName: "io.mender.Authentication1",
			methodName:    "GetJwtToken",
			callback: func(objectPath, interfaceName, methodName string) (interface{}, error) {
				return "JWT_TOKEN", nil
			},
			outString: "JWT_TOKEN",
		},
		"ok, bool value true": {
			xml:           xml,
			path:          "/io/mender/AuthenticationManager/TestHandleMethodCallCallback2",
			interfaceName: "io.mender.Authentication1",
			methodName:    "FetchJwtToken",
			callback: func(objectPath, interfaceName, methodName string) (interface{}, error) {
				return true, nil
			},
			outBoolean: true,
		},
		"ok, bool value false": {
			xml:           xml,
			path:          "/io/mender/AuthenticationManager/TestHandleMethodCallCallback3",
			interfaceName: "io.mender.Authentication1",
			methodName:    "FetchJwtToken",
			callback: func(objectPath, interfaceName, methodName string) (interface{}, error) {
				return false, nil
			},
			outBoolean: false,
		},
	}
	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			gid, err = libgio.BusRegisterInterface(conn, tc.path, tc.xml)
			assert.NoError(t, err)
			assert.Greater(t, gid, uint(0))

			libgio.RegisterMethodCallCallback(tc.path, tc.interfaceName, tc.methodName, tc.callback)

			loop := libgio.MainLoopNew()
			go libgio.MainLoopRun(loop)
			defer libgio.MainLoopQuit(loop)

			// let the dbus-daemon to set up
			time.Sleep(500 * time.Millisecond)

			// client code, call the dbus method, isolated from the code above
			func() {
				if tc.outString != "" {
					var value string
					interfaceMethodName := fmt.Sprintf("%s.%s", tc.interfaceName, tc.methodName)
					err = godbusConn.Object(objectName, godbus.ObjectPath(tc.path)).Call(interfaceMethodName, 0).Store(&value)
					assert.NoError(t, err)
					assert.Equal(t, tc.outString, value)
				} else {
					var value bool
					interfaceMethodName := fmt.Sprintf("%s.%s", tc.interfaceName, tc.methodName)
					err = godbusConn.Object(objectName, godbus.ObjectPath(tc.path)).Call(interfaceMethodName, 0).Store(&value)
					assert.NoError(t, err)
					assert.Equal(t, tc.outBoolean, value)
				}
			}()
		})

		// let the dbus-daemon to clean up
		time.Sleep(500 * time.Millisecond)
	}
}

func TestMainLoop(t *testing.T) {
	loop := libgio.MainLoopNew()
	go libgio.MainLoopRun(loop)
	defer libgio.MainLoopQuit(loop)

	time.Sleep(100 * time.Millisecond)
}

func TestEmitSignal(t *testing.T) {
	conn, err := libgio.BusGet(GBusTypeSystem)
	assert.NoError(t, err)
	assert.NotNil(t, conn)

	const objectName = "io.mender.AuthenticationManager"

	gid, err := libgio.BusOwnNameOnConnection(conn, objectName,
		DBusNameOwnerFlagsAllowReplacement|DBusNameOwnerFlagsReplace)
	assert.NoError(t, err)
	assert.Greater(t, gid, uint(0))

	godbusConn, err := godbus.SystemBus()
	assert.NoError(t, err)
	defer godbusConn.Close()

	xml := `<node>
	<interface name="io.mender.Authentication1">
		<signal name="ValidJwtTokenAvailable">
			<arg type="b" name="success" direction="out"/>
		</signal>
	</interface>
</node>`

	testCases := map[string]struct {
		objectPath    string
		interfaceName string
		signalName    string
		err           error
	}{
		"ok": {
			objectPath:    "/io/mender/AuthenticationManager/TestEmitSignal1",
			interfaceName: "io.mender.Authentication1",
			signalName:    "ValidJwtTokenAvailable",
		},
	}
	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			gid, err = libgio.BusRegisterInterface(conn, tc.objectPath, xml)
			assert.NoError(t, err)
			assert.Greater(t, gid, uint(0))

			loop := libgio.MainLoopNew()
			go libgio.MainLoopRun(loop)
			defer libgio.MainLoopQuit(loop)

			// let the dbus-daemon to set up
			time.Sleep(500 * time.Millisecond)

			err = libgio.EmitSignal(conn, objectName, tc.objectPath, tc.interfaceName, tc.signalName)
			if tc.err != nil {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})

		// let the dbus-daemon to clean up
		time.Sleep(500 * time.Millisecond)
	}
}
