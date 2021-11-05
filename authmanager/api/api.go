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

package api

import (
	"strings"

	common "github.com/mendersoftware/mender/common/api"
	"github.com/mendersoftware/mender/common/dbus"

	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
)

const (
	ProxyDBusPath                = "/io/mender/Proxy"
	ProxyDBusObjectName          = "io.mender.Proxy"
	ProxyDBusInterfaceName       = "io.mender.Proxy1"
	ProxyDBusSetupServerURLProxy = "SetupServerURLProxy"
)

type ProxyServerURLSetupper interface {
	SetupServerURLProxy(serverURL, jwtToken string) (string, error)
}

type ApiAuthManager struct {
	DBusAPI dbus.DBusAPI
}

func NewApiAuthManager(dbus dbus.DBusAPI) *ApiAuthManager {
	aAuth := &ApiAuthManager{
		DBusAPI: dbus,
	}

	return aAuth
}

func urlServer(server string) string {
	if strings.HasPrefix(server, "https://") || strings.HasPrefix(server, "http://") {
		return server
	}
	return "https://" + server
}

func BuildApiURL(server, url string) string {
	return strings.TrimRight(urlServer(server), "/") +
		common.ApiPrefix + strings.TrimLeft(url, "/")
}

func (a *ApiAuthManager) SetupServerURLProxy(serverURL, jwtToken string) (string, error) {
	bus, err := a.DBusAPI.BusGet(dbus.GBusTypeSystem)
	if err != nil {
		return "", errors.Wrap(err, "Could not setup server proxy")
	}

	params, err := a.DBusAPI.Call(
		bus,
		ProxyDBusObjectName,
		ProxyDBusPath,
		ProxyDBusInterfaceName,
		ProxyDBusSetupServerURLProxy,
		serverURL,
		jwtToken)
	if err != nil {
		return "", errors.Wrap(err, "Could not setup server proxy")
	}

	return a.parseProxyURLFromDbus(params)
}

func (a *ApiAuthManager) parseProxyURLFromDbus(params []interface{}) (string, error) {
	if len(params) != 1 {
		return "", errors.New("Unexpected D-Bus JWT information: Contained different than one element")
	}
	url, urlOk := params[0].(string)
	if !urlOk {
		return "", errors.Errorf("Unexpected D-Bus JWT information: Expected one string, but type is %T",
			params[0])
	}

	if url == "" {
		log.Debug("Received empty proxy URL from D-Bus")
	} else {
		log.Debugf("Received proxy URL %s from D-Bus", url)
	}

	return url, nil
}
