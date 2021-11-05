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
package app

import (
	"context"

	log "github.com/sirupsen/logrus"

	"github.com/pkg/errors"

	"github.com/mendersoftware/mender/client/conf"
	"github.com/mendersoftware/mender/client/proxy"
	"github.com/mendersoftware/mender/common/dbus"
	"github.com/mendersoftware/mender/common/tls"
)

const (
	ProxyDBusPath                = "/io/mender/Proxy"
	ProxyDBusObjectName          = "io.mender.Proxy"
	ProxyDBusInterfaceName       = "io.mender.Proxy1"
	ProxyDBusSetupServerURLProxy = "SetupServerURLProxy"
	ProxyDBusInterface           = `<node>
	<interface name="io.mender.Proxy1">
		<method name="SetupServerURLProxy">
			<arg type="s" name="server_url" direction="in"/>
			<arg type="s" name="token" direction="in"/>
			<arg type="s" name="proxy_url" direction="out"/>
		</method>
	</interface>
</node>`
)

type ProxyManager struct {
	DBusAPI dbus.DBusAPI
	proxy   *proxy.ProxyController
}

func NewProxyManager(config *conf.MenderConfig) (*ProxyManager, error) {
	client, err := tls.NewHttpOrHttpsClient(config.GetHttpConfig())
	if err != nil {
		return nil, err
	}

	wsDialer, err := tls.NewWebsocketDialer(config.GetHttpConfig())
	if err != nil {
		return nil, err
	}

	proxy, err := proxy.NewProxyController(client, wsDialer, "", "")
	if err != nil {
		return nil, err
	}

	m := &ProxyManager{
		DBusAPI: dbus.GetDBusAPI(),
		proxy:   proxy,
	}

	return m, nil
}

func (pm *ProxyManager) Start() (context.CancelFunc, error) {
	log.Debug("Running the ProxyManager")
	if pm.DBusAPI == nil {
		return nil, errors.New("DBus not enabled")
	}
	ctx, cancel := context.WithCancel(context.Background())
	go pm.run(ctx)
	return cancel, nil
}

func (pm *ProxyManager) run(ctx context.Context) error {
	log.Debug("Running the ProxyManager")
	dbusConn, err := pm.DBusAPI.BusGet(dbus.GBusTypeSystem)
	if err != nil {
		return errors.Wrap(err, "Failed to start the Proxy manager")
	}
	nameGID, err := pm.DBusAPI.BusOwnNameOnConnection(
		dbusConn,
		ProxyDBusObjectName,
		dbus.DBusNameOwnerFlagsAllowReplacement|dbus.DBusNameOwnerFlagsReplace,
	)
	if err != nil {
		return errors.Wrap(err, "Failed to start the Proxy manager")
	}
	defer pm.DBusAPI.BusUnownName(nameGID)
	intGID, err := pm.DBusAPI.BusRegisterInterface(
		dbusConn,
		ProxyDBusPath,
		ProxyDBusInterface,
	)
	if err != nil {
		log.Errorf("Failed to register the DBus interface name %q at path %q: %s",
			ProxyDBusInterface,
			ProxyDBusPath,
			err,
		)
		return err
	}
	defer pm.DBusAPI.BusUnregisterInterface(dbusConn, intGID)

	// SetupServerURLProxy
	pm.DBusAPI.RegisterMethodCallCallback(
		ProxyDBusPath,
		ProxyDBusInterfaceName,
		ProxyDBusSetupServerURLProxy,
		func(_, _, _ string, parameters []interface{}) ([]interface{}, error) {
			if len(parameters) != 2 {
				return nil, errors.Errorf("Expected 2 arguments, got %d", len(parameters))
			}
			var params []string
			for _, entry := range parameters {
				switch e := entry.(type) {
				case string:
					params = append(params, e)
				default:
					return nil, errors.Errorf("Unsupported DBus encoding type: %T", entry)
				}
			}

			err := pm.reconfigure(params[0], params[1])
			if err != nil {
				return nil, errors.Errorf("error reconfiguring proxy: %s", err.Error())
			}
			return []interface{}{pm.proxy.GetServerUrl()}, nil
		})
	defer pm.DBusAPI.UnregisterMethodCallCallback(
		ProxyDBusPath,
		ProxyDBusInterfaceName,
		ProxyDBusSetupServerURLProxy)

	log.Infof("%s Registered successfully", ProxyDBusInterfaceName)

	<-ctx.Done()

	log.Infof("%s Done, exiting", ProxyDBusInterfaceName)
	return nil
}

func (pm *ProxyManager) reconfigure(menderUrl, menderJwtToken string) error {
	pm.proxy.Stop()

	err := pm.proxy.Reconfigure(menderUrl, menderJwtToken)
	if err != nil {
		return err
	}

	pm.proxy.Start()
	return nil
}
