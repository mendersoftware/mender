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

package app

import (
	"github.com/mendersoftware/mender/client"
	"github.com/mendersoftware/mender/conf"
	log "github.com/sirupsen/logrus"
)

// see client.go: ApiRequest.Do()

// nextServerIterator returns an iterator like function that cycles through the
// list of available servers in mender.conf.MenderConfig.Servers
func nextServerIterator(config conf.MenderConfig) func() *client.MenderServer {
	numServers := len(config.Servers)
	if config.Servers == nil || numServers == 0 {
		log.Error("Empty server list! Make sure at least one server" +
			"is specified in /etc/mender/mender.conf")
		return nil
	}

	idx := 0
	return func() (server *client.MenderServer) {
		var ret *client.MenderServer
		if idx < numServers {
			ret = &config.Servers[idx]
			idx++
		} else {
			// return nil which terminates Do()
			// and reset index (for reuse of request)
			ret = nil
			idx = 0
		}
		return ret
	}
}
