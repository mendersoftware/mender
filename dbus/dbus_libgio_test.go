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

import (
	"fmt"
	"testing"
	"time"

	godbus "github.com/godbus/dbus"
	"github.com/stretchr/testify/assert"
)

var libgio *dbusAPILibGio

func setDBusAPI(api DBusAPI) {
	dbusAPI = api
}

func libgioTestSetup() {
	libgio = &dbusAPILibGio{
		MethodCallCallbacks: make(map[string]MethodCallCallback),
	}
	setDBusAPI(libgio)
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
	conn, err := libgio.BusGet(GBusTypeSession)
	assert.NoError(t, err)
	assert.NotNil(t, conn)
}

func TestBusOwnNameOnConnection(t *testing.T) {
	testCases := []struct {
		connectionName string
	}{
		{
			connectionName: "io.mender.AuthenticationManager",
		},
		{
			connectionName: "io.mender.UpdateManager",
		},
	}
	for _, test := range testCases {
		conn, err := libgio.BusGet(GBusTypeSession)
		assert.NoError(t, err)
		assert.NotNil(t, conn)

		gid, err := libgio.BusOwnNameOnConnection(conn, test.connectionName, DBusNameOwnerFlagsNone)
		assert.NoError(t, err)
		assert.Greater(t, gid, uint(0))
		defer libgio.BusUnownName(gid)
	}
}

func TestBusRegisterInterface(t *testing.T) {
	testCases := map[string]struct {
		xml            string
		path           string
		err            bool
		connectionName string
	}{
		// io.mender.AuthenticationManager
		"ok": {
			connectionName: "io.mender.AuthenticationManager",
			xml: `<node>
			<interface name="io.mender.Authentication1">
				<method name="GetJwtToken">
					<arg type="s" name="token" direction="out"/>
					<arg type="s" name="server_url" direction="out"/>
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
			connectionName: "io.mender.AuthenticationManager",
			xml:            "dummy-interface",
			path:           "/io/mender/AuthenticationManager/TestBusRegisterInterface2",
			err:            true,
		},
		"ko, invalid path": {
			connectionName: "io.mender.AuthenticationManager",
			xml: `<node>
			<interface name="io.mender.Authentication1">
				<method name="GetJwtToken">
					<arg type="s" name="token" direction="out"/>
					<arg type="s" name="server_url" direction="out"/>
				</method>
				<method name="FetchJwtToken">
					<arg type="b" name="success" direction="out"/>
				</method>
			</interface>
		</node>`,
			path: "io/mender/AuthenticationManager/TestBusRegisterInterface3",
			err:  true,
		},

		// io.mender.UpdateManager
		"ok, UpdateManager interface": {
			connectionName: "io.mender.UpdateManager",
			xml: `
                 <node>
		    <interface name="io.mender.Update1">
			    <method name="SetUpdateControlMap">
				    <arg type="s" name="update_control_map" direction="in"/>
				    <arg type="i" name="refresh_timeout" direction="out"/>
			    </method>
		    </interface>
		</node>`,
			path: "/io/mender/UpdateManager/TestBusRegisterInterface1",
			err:  false,
		},
	}
	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			conn, err := libgio.BusGet(GBusTypeSession)
			assert.NoError(t, err)
			assert.NotNil(t, conn)

			nameGid, err := libgio.BusOwnNameOnConnection(
				conn,
				tc.connectionName,
				DBusNameOwnerFlagsNone,
			)
			assert.NoError(t, err)
			assert.Greater(t, nameGid, uint(0))
			defer libgio.BusUnownName(nameGid)

			intGid, err := libgio.BusRegisterInterface(conn, tc.path, tc.xml)
			if tc.err {
				assert.Error(t, err)
				assert.Equal(t, intGid, uint(0))
			} else {
				assert.NoError(t, err)
				assert.Greater(t, intGid, uint(0))
				defer libgio.BusUnregisterInterface(conn, intGid)
			}
		})
	}
}

func TestRegisterMethodCallCallback(t *testing.T) {
	callback := func(objectPath string, interfaceName string, methodName string, parameters string) (interface{}, error) {
		return "value", nil
	}

	path := "/io/mender/AuthenticationManager"
	interfaceName := "io.mender.Authentication1"
	methodName := "GetJwtToken"
	libgio.RegisterMethodCallCallback(path, interfaceName, methodName, callback)
	defer libgio.UnregisterMethodCallCallback(path, interfaceName, methodName)

	key := keyForPathInterfaceNameAndMethod(path, interfaceName, methodName)
	_, ok := libgio.MethodCallCallbacks[key]
	assert.True(t, ok)

	key = keyForPathInterfaceNameAndMethod(path, interfaceName, "dummyMethod")
	_, ok = libgio.MethodCallCallbacks[key]
	assert.False(t, ok)
}

func TestHandleMethodCallCallback(t *testing.T) {
	conn, err := libgio.BusGet(GBusTypeSession)
	assert.NoError(t, err)
	assert.NotNil(t, conn)

	const objectName = "io.mender.AuthenticationManager"

	gid, err := libgio.BusOwnNameOnConnection(conn, objectName,
		DBusNameOwnerFlagsAllowReplacement|DBusNameOwnerFlagsReplace)
	assert.NoError(t, err)
	assert.Greater(t, gid, uint(0))
	defer libgio.BusUnownName(gid)

	godbusConn, err := godbus.SessionBus()
	assert.NoError(t, err)
	defer godbusConn.Close()

	xmlString := `<node>
	<interface name="io.mender.Authentication1">
		<method name="GetJwtToken">
			<arg type="s" name="token" direction="out"/>
		</method>
		<method name="FetchJwtToken">
			<arg type="b" name="success" direction="out"/>
		</method>
	</interface>
</node>`

	xml := `<node>
	<interface name="io.mender.Authentication1">
		<method name="GetJwtToken">
			<arg type="s" name="token" direction="out"/>
			<arg type="s" name="server_url" direction="out"/>
		</method>
		<method name="FetchJwtToken">
			<arg type="b" name="success" direction="out"/>
		</method>
	</interface>
</node>`

	testCases := map[string]struct {
		xml                  string
		path                 string
		interfaceName        string
		methodName           string
		callback             MethodCallCallback
		outTokenAndServerURL *TokenAndServerURL
		outString            string
		outBoolean           bool
	}{
		"ok, string value": {
			xml:           xmlString,
			path:          "/io/mender/AuthenticationManager/TestHandleMethodCallCallback1",
			interfaceName: "io.mender.Authentication1",
			methodName:    "GetJwtToken",
			callback: func(objectPath, interfaceName, methodName, parameters string) (interface{}, error) {
				return "JWT_TOKEN", nil
			},
			outString: "JWT_TOKEN",
		},
		"ok, bool value true": {
			xml:           xml,
			path:          "/io/mender/AuthenticationManager/TestHandleMethodCallCallback2",
			interfaceName: "io.mender.Authentication1",
			methodName:    "FetchJwtToken",
			callback: func(objectPath, interfaceName, methodName, parameters string) (interface{}, error) {
				return true, nil
			},
			outBoolean: true,
		},
		"ok, bool value false": {
			xml:           xml,
			path:          "/io/mender/AuthenticationManager/TestHandleMethodCallCallback3",
			interfaceName: "io.mender.Authentication1",
			methodName:    "FetchJwtToken",
			callback: func(objectPath, interfaceName, methodName, parameters string) (interface{}, error) {
				return false, nil
			},
			outBoolean: false,
		},
		"ok, value": {
			xml:           xml,
			path:          "/io/mender/AuthenticationManager/TestHandleMethodCallCallback4",
			interfaceName: "io.mender.Authentication1",
			methodName:    "GetJwtToken",
			callback: func(objectPath, interfaceName, methodName, parameters string) (interface{}, error) {
				return TokenAndServerURL{Token: "JWT_TOKEN", ServerURL: "SERVER_URL"}, nil
			},
			outTokenAndServerURL: &TokenAndServerURL{Token: "JWT_TOKEN", ServerURL: "SERVER_URL"},
		},
	}
	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			gid, err = libgio.BusRegisterInterface(conn, tc.path, tc.xml)
			assert.NoError(t, err)
			assert.Greater(t, gid, uint(0))
			defer libgio.BusUnregisterInterface(conn, gid)

			libgio.RegisterMethodCallCallback(tc.path, tc.interfaceName, tc.methodName, tc.callback)
			defer libgio.UnregisterMethodCallCallback(tc.path, tc.interfaceName, tc.methodName)

			loop := libgio.MainLoopNew()
			go libgio.MainLoopRun(loop)
			defer libgio.MainLoopQuit(loop)

			// client code, call the dbus method, isolated from the code above
			func() {
				if tc.outTokenAndServerURL != nil {
					var valueToken string
					var valueServerURL string
					interfaceMethodName := fmt.Sprintf("%s.%s", tc.interfaceName, tc.methodName)
					err = godbusConn.Object(objectName, godbus.ObjectPath(tc.path)).
						Call(interfaceMethodName, 0).
						Store(&valueToken, &valueServerURL)
					assert.NoError(t, err)
					assert.Equal(t, tc.outTokenAndServerURL.Token, valueToken)
					assert.Equal(t, tc.outTokenAndServerURL.ServerURL, valueServerURL)
				} else if tc.outString != "" {
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
	}
}

func TestMainLoop(t *testing.T) {
	loop := libgio.MainLoopNew()
	go libgio.MainLoopRun(loop)
	defer libgio.MainLoopQuit(loop)

	time.Sleep(100 * time.Millisecond)
}

func TestEmitSignal(t *testing.T) {
	conn, err := libgio.BusGet(GBusTypeSession)
	assert.NoError(t, err)
	assert.NotNil(t, conn)

	const objectName = "io.mender.AuthenticationManager"

	gid, err := libgio.BusOwnNameOnConnection(conn, objectName,
		DBusNameOwnerFlagsAllowReplacement|DBusNameOwnerFlagsReplace)
	assert.NoError(t, err)
	assert.Greater(t, gid, uint(0))
	defer libgio.BusUnownName(gid)

	godbusConn, err := godbus.SessionBus()
	assert.NoError(t, err)
	defer godbusConn.Close()

	xml := `<node>
	<interface name="io.mender.Authentication1">
		<signal name="JwtTokenStateChange">
			<arg type="s" name="token"/>
		</signal>
	</interface>
</node>`

	testCases := map[string]struct {
		objectName    string
		objectPath    string
		interfaceName string
		signalName    string
		err           error
	}{
		"ok": {
			objectName:    objectName,
			objectPath:    "/io/mender/AuthenticationManager/TestEmitSignal1",
			interfaceName: "io.mender.Authentication1",
			signalName:    "JwtTokenStateChange",
		},
		"ok, broadcast": {
			objectName:    "",
			objectPath:    "/io/mender/AuthenticationManager/TestEmitSignal2",
			interfaceName: "io.mender.Authentication1",
			signalName:    "JwtTokenStateChange",
		},
	}
	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			gid, err = libgio.BusRegisterInterface(conn, tc.objectPath, xml)
			assert.NoError(t, err)
			assert.Greater(t, gid, uint(0))
			defer libgio.BusUnregisterInterface(conn, gid)

			loop := libgio.MainLoopNew()
			go libgio.MainLoopRun(loop)
			defer libgio.MainLoopQuit(loop)

			err = libgio.EmitSignal(
				conn,
				tc.objectName,
				tc.objectPath,
				tc.interfaceName,
				tc.signalName,
				"token",
			)
			if tc.err != nil {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
