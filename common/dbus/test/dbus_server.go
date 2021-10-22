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
package test

// #cgo pkg-config: gio-2.0
// #include <gio/gio.h>
import "C"
import (
	"fmt"
	"io/ioutil"
	"os"
	"path"
	"runtime"
	"syscall"
	"time"
	"unsafe"

	dbus "github.com/mendersoftware/mender/common/dbus/dbus_internal"
	"github.com/mendersoftware/mender/common/system"

	"github.com/pkg/errors"
)

type DBusTestServer struct {
	tmpdir  string
	cmd     *system.Cmd
	busAddr string
}

func NewDBusTestServer() *DBusTestServer {
	var dbusServer DBusTestServer
	var err error
	dbusServer.tmpdir, err = ioutil.TempDir("", "mender-test-dbus-daemon")
	if err != err {
		panic(fmt.Sprintf("Could not create temporary directory: %s", err.Error()))
	}
	dbusSocket := path.Join(dbusServer.tmpdir, "bus")
	dbusServer.busAddr = fmt.Sprintf("unix:path=%s", dbusSocket)

	dbusServer.cmd = system.Command("dbus-daemon", "--session", "--address="+dbusServer.busAddr)
	err = dbusServer.cmd.Start()
	if err != nil {
		panic(fmt.Sprintf("Could not start test DBus server: %s", err.Error()))
	}

	// Wait until server is up.
	attempts := 100
	attempt := 0
	for attempt = 0; attempt < attempts; attempt++ {
		if _, err = os.Stat(dbusSocket); !errors.Is(err, os.ErrNotExist) {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if attempt >= attempts {
		dbusServer.Close()
		panic("Could not start DBus server: timed out")
	}

	return &dbusServer
}

func (s *DBusTestServer) Close() {
	s.cmd.Process.Signal(syscall.SIGTERM)
	s.cmd.Wait()

	os.RemoveAll(s.tmpdir)
}

func (s *DBusTestServer) GetAddress() string {
	return s.busAddr
}

func (s *DBusTestServer) GetDBusAPI() *DBusTestAPI {
	return &DBusTestAPI{
		DBusAPI: dbus.NewDBusAPI(),
		busAddr: s.busAddr,
	}
}

type DBusTestAPI struct {
	dbus.DBusAPI
	busAddr string
	conn    dbus.Handle
}

func (api *DBusTestAPI) BusGet(bus uint) (dbus.Handle, error) {
	if api.conn != nil {
		return api.conn, nil
	}

	var gerror *C.GError
	gBusAddr := C.CString(api.busAddr)
	defer C.free(unsafe.Pointer(gBusAddr))
	gconn := C.g_dbus_connection_new_for_address_sync(
		gBusAddr,
		C.G_DBUS_CONNECTION_FLAGS_AUTHENTICATION_CLIENT|
			C.G_DBUS_CONNECTION_FLAGS_MESSAGE_BUS_CONNECTION,
		nil, // observer
		nil, // cancellable
		&gerror,
	)
	if dbus.Handle(gerror) != nil {
		return nil, dbus.ErrorFromNative(dbus.Handle(gerror))
	}

	if gconn == nil {
		return nil, errors.New("g_dbus_connection_new_for_address_sync returned NULL connection")
	}

	api.conn = dbus.Handle(gconn)
	runtime.SetFinalizer(api, func(api *DBusTestAPI) {
		C.g_object_unref(C.gpointer(api.conn))
	})

	return api.conn, nil
}
