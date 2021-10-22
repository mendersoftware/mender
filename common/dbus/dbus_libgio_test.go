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
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenerateGUID(t *testing.T) {
	guid := dbusAPITest.GenerateGUID()
	assert.NotEmpty(t, guid)
}

func TestIsGUID(t *testing.T) {
	// Dummy GUID
	assert.False(t, dbusAPITest.IsGUID("fake-guid"))

	// Get and check a valid GUID
	guid := dbusAPITest.GenerateGUID()
	assert.True(t, dbusAPITest.IsGUID(guid))
}

func TestBusGet(t *testing.T) {
	conn, err := dbusAPITest.BusGet(GBusTypeSystem)
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
		conn, err := dbusAPITest.BusGet(GBusTypeSystem)
		assert.NoError(t, err)
		assert.NotNil(t, conn)

		gid, err := dbusAPITest.BusOwnNameOnConnection(conn, test.connectionName, DBusNameOwnerFlagsNone)
		assert.NoError(t, err)
		assert.Greater(t, gid, uint(0))
		defer dbusAPITest.BusUnownName(gid)
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
			conn, err := dbusAPITest.BusGet(GBusTypeSystem)
			assert.NoError(t, err)
			assert.NotNil(t, conn)

			nameGid, err := dbusAPITest.BusOwnNameOnConnection(conn, tc.connectionName, DBusNameOwnerFlagsNone)
			assert.NoError(t, err)
			assert.Greater(t, nameGid, uint(0))
			defer dbusAPITest.BusUnownName(nameGid)

			intGid, err := dbusAPITest.BusRegisterInterface(conn, tc.path, tc.xml)
			if tc.err {
				assert.Error(t, err)
				assert.Equal(t, intGid, uint(0))
			} else {
				assert.NoError(t, err)
				assert.Greater(t, intGid, uint(0))
				defer dbusAPITest.BusUnregisterInterface(conn, intGid)
			}
		})
	}
}

func TestRegisterMethodCallCallback(t *testing.T) {
	callback := func(objectPath string, interfaceName string, methodName string, parameters string) ([]interface{}, error) {
		return []interface{}{"value"}, nil
	}

	path := "/io/mender/AuthenticationManager"
	interfaceName := "io.mender.Authentication1"
	methodName := "GetJwtToken"
	dbusAPITest.RegisterMethodCallCallback(path, interfaceName, methodName, callback)
	defer dbusAPITest.UnregisterMethodCallCallback(path, interfaceName, methodName)

	key := keyForPathInterfaceNameAndMethod(path, interfaceName, methodName)
	_, ok := dbusAPITest.DBusAPI.(*dbusAPILibGio).MethodCallCallbacks[key]
	assert.True(t, ok)

	key = keyForPathInterfaceNameAndMethod(path, interfaceName, "dummyMethod")
	_, ok = dbusAPITest.DBusAPI.(*dbusAPILibGio).MethodCallCallbacks[key]
	assert.False(t, ok)
}

func TestHandleMethodCallCallback(t *testing.T) {
	conn, err := dbusAPITest.BusGet(GBusTypeSystem)
	assert.NoError(t, err)
	assert.NotNil(t, conn)

	const objectName = "io.mender.AuthenticationManager"

	gid, err := dbusAPITest.BusOwnNameOnConnection(conn, objectName,
		DBusNameOwnerFlagsAllowReplacement|DBusNameOwnerFlagsReplace)
	assert.NoError(t, err)
	assert.Greater(t, gid, uint(0))
	defer dbusAPITest.BusUnownName(gid)

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
		xml           string
		path          string
		interfaceName string
		methodName    string
		callback      MethodCallCallback
		outToken      string
		outServerURL  string
		outString     string
		outBoolean    bool
	}{
		"ok, string value": {
			xml:           xmlString,
			path:          "/io/mender/AuthenticationManager/TestHandleMethodCallCallback1",
			interfaceName: "io.mender.Authentication1",
			methodName:    "GetJwtToken",
			callback: func(objectPath, interfaceName, methodName, parameters string) ([]interface{}, error) {
				return []interface{}{"JWT_TOKEN"}, nil
			},
			outString: "JWT_TOKEN",
		},
		"ok, bool value true": {
			xml:           xml,
			path:          "/io/mender/AuthenticationManager/TestHandleMethodCallCallback2",
			interfaceName: "io.mender.Authentication1",
			methodName:    "FetchJwtToken",
			callback: func(objectPath, interfaceName, methodName, parameters string) ([]interface{}, error) {
				return []interface{}{true}, nil
			},
			outBoolean: true,
		},
		"ok, bool value false": {
			xml:           xml,
			path:          "/io/mender/AuthenticationManager/TestHandleMethodCallCallback3",
			interfaceName: "io.mender.Authentication1",
			methodName:    "FetchJwtToken",
			callback: func(objectPath, interfaceName, methodName, parameters string) ([]interface{}, error) {
				return []interface{}{false}, nil
			},
			outBoolean: false,
		},
		"ok, value": {
			xml:           xml,
			path:          "/io/mender/AuthenticationManager/TestHandleMethodCallCallback4",
			interfaceName: "io.mender.Authentication1",
			methodName:    "GetJwtToken",
			callback: func(objectPath, interfaceName, methodName, parameters string) ([]interface{}, error) {
				return []interface{}{"JWT_TOKEN", "SERVER_URL"}, nil
			},
			outToken:     "JWT_TOKEN",
			outServerURL: "SERVER_URL",
		},
	}
	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			gid, err = dbusAPITest.BusRegisterInterface(conn, tc.path, tc.xml)
			assert.NoError(t, err)
			assert.Greater(t, gid, uint(0))
			defer dbusAPITest.BusUnregisterInterface(conn, gid)

			dbusAPITest.RegisterMethodCallCallback(tc.path, tc.interfaceName, tc.methodName, tc.callback)
			defer dbusAPITest.UnregisterMethodCallCallback(tc.path, tc.interfaceName, tc.methodName)

			loop := dbusAPITest.MainLoopNew()
			go dbusAPITest.MainLoopRun(loop)
			defer dbusAPITest.MainLoopQuit(loop)

			// client code, call the dbus method, isolated from the code above
			func() {
				if tc.outToken != "" {
					values, err := dbusAPITest.Call0(conn, objectName, tc.path, tc.interfaceName, tc.methodName)
					require.NoError(t, err)
					require.Equal(t, len(values), 2)
					require.IsType(t, "", values[0])
					require.IsType(t, "", values[1])
					assert.Equal(t, tc.outToken, values[0])
					assert.Equal(t, tc.outServerURL, values[1])
				} else if tc.outString != "" {
					values, err := dbusAPITest.Call0(conn, objectName, tc.path, tc.interfaceName, tc.methodName)
					require.NoError(t, err)
					require.Equal(t, len(values), 1)
					require.IsType(t, "", values[0])
					assert.Equal(t, tc.outString, values[0])
				} else {
					values, err := dbusAPITest.Call0(conn, objectName, tc.path, tc.interfaceName, tc.methodName)
					require.NoError(t, err)
					require.Equal(t, len(values), 1)
					require.IsType(t, true, values[0])
					assert.Equal(t, tc.outBoolean, values[0])
				}
			}()
		})
	}
}

func TestMainLoop(t *testing.T) {
	loop := dbusAPITest.MainLoopNew()
	go dbusAPITest.MainLoopRun(loop)
	defer dbusAPITest.MainLoopQuit(loop)

	time.Sleep(100 * time.Millisecond)
}

func TestEmitSignal(t *testing.T) {
	conn, err := dbusAPITest.BusGet(GBusTypeSystem)
	assert.NoError(t, err)
	assert.NotNil(t, conn)

	const objectName = "io.mender.AuthenticationManager"

	gid, err := dbusAPITest.BusOwnNameOnConnection(conn, objectName,
		DBusNameOwnerFlagsAllowReplacement|DBusNameOwnerFlagsReplace)
	assert.NoError(t, err)
	assert.Greater(t, gid, uint(0))
	defer dbusAPITest.BusUnownName(gid)

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
			gid, err = dbusAPITest.BusRegisterInterface(conn, tc.objectPath, xml)
			assert.NoError(t, err)
			assert.Greater(t, gid, uint(0))
			defer dbusAPITest.BusUnregisterInterface(conn, gid)

			loop := dbusAPITest.MainLoopNew()
			go dbusAPITest.MainLoopRun(loop)
			defer dbusAPITest.MainLoopQuit(loop)

			err = dbusAPITest.EmitSignal(conn, tc.objectName, tc.objectPath, tc.interfaceName, tc.signalName, "token")
			if tc.err != nil {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
