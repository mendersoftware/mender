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

import (
	"context"
)

func RegisterAndServeIoMenderProxy(dbusServer *DBusTestServer, ctx context.Context, proxyUrl string) {
	dbusServer.RegisterAndServeDBusInterface(&DBusInterfaceProperties{
		ObjectName: "io.mender.Proxy",
		ObjectPath: "/io/mender/Proxy",
		InterfaceSpec: `<node>
	<interface name="io.mender.Proxy1">
		<method name="SetupServerURLProxy">
			<arg type="s" name="server_url" direction="in"/>
			<arg type="s" name="token" direction="in"/>
			<arg type="s" name="proxy_url" direction="out"/>
		</method>
	</interface>
</node>`,
		InterfaceName: "io.mender.Proxy1",
		Methods: []*DBusMethodProperties{{
			Name: "SetupServerURLProxy",
			Callback: func(_, _, _ string, parameters []interface{}) ([]interface{}, error) {
				return []interface{}{proxyUrl}, nil
			},
		}},
	}, ctx)
}
