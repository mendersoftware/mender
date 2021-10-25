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
package conf

import (
	"strings"

	common "github.com/mendersoftware/mender/common/conf"

	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
)

// MenderServer is a placeholder for a full server definition used when
// multiple servers are given. The fields corresponds to the definitions
// given in MenderConfig.
type MenderServer struct {
	ServerURL string
	// TODO: Move all possible server specific configurations in
	//       MenderConfig over to this struct. (e.g. TenantToken?)
}

type AuthConfig struct {
	common.Config

	// Server URL (For single server conf)
	ServerURL string `json:",omitempty"`
	// Server JWT TenantToken
	TenantToken string `json:",omitempty"`
	// List of available servers, to which client can fall over
	Servers []MenderServer `json:",omitempty"`
}

func NewAuthConfig() *AuthConfig {
	return &AuthConfig{
		Config: *common.NewConfig(),
	}
}

// Validate verifies the Servers fields in the configuration
func (c *AuthConfig) Validate() error {
	if c.Servers == nil {
		if c.ServerURL == "" {
			log.Warn("No server URL(s) specified in mender configuration.")
		}
		c.Servers = make([]MenderServer, 1)
		c.Servers[0].ServerURL = c.ServerURL
	} else if c.ServerURL != "" {
		log.Error("In mender.conf: don't specify both Servers field " +
			"AND the corresponding fields in base structure (i.e. " +
			"ServerURL). The first server on the list overwrites" +
			"these fields.")
		return errors.New("Both Servers AND ServerURL given in " +
			"mender.conf")
	}
	for i := 0; i < len(c.Servers); i++ {
		// Trim possible '/' suffix, which is added back in URL path
		if strings.HasSuffix(c.Servers[i].ServerURL, "/") {
			c.Servers[i].ServerURL =
				strings.TrimSuffix(
					c.Servers[i].ServerURL, "/")
		}
		if c.Servers[i].ServerURL == "" {
			log.Warnf("Server entry %d has no associated server URL.", i+1)
		}
	}

	c.HttpsClient.Validate()

	if c.HttpsClient.Key != "" && c.Security.AuthPrivateKey != "" {
		log.Warn("both config.HttpsClient.Key and config.Security.AuthPrivateKey" +
			" specified; config.Security.AuthPrivateKey will take precedence over" +
			" the former for the signing of auth requests.")
	}

	log.Debugf("Verified configuration = %#v", c)

	return nil
}

func (c *AuthConfig) CheckConfigDefaults() {
	// Nothing to check for AuthManager.
}
