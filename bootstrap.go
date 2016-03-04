// Copyright 2016 Mender Software AS
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
package main

import "errors"
import "github.com/mendersoftware/log"
import "io/ioutil"
import "net/http"
import "crypto/tls"
import "crypto/x509"

//TODO: this will be hardcoded for now but should be configurable in future
const (
	defaultCertFile   = "/data/certfile.crt"
	defaultCertKey    = "/data/certkey.key"
	defaultServerCert = "/data/servercert.crt"
)

type Client struct {
  BaseURL string
  HTTPClient *http.Client
}

type authCredsType struct {
	// hostname or address to bootstrap to
	bootstrapServer *string
	// Cert+privkey that authenticates this client
	clientCert tls.Certificate
	// Trusted server certificates
	trustedCerts x509.CertPool

	certFile   *string
	certKey    *string
	serverCert *string
}

func (cred *authCredsType) setDefaultKeysAndCerts(clientCert string, clientKey string, serverCert string) {
	if *cred.certFile == "" {
		cred.certFile = &clientCert
	}
	if *cred.certKey == "" {
		cred.certKey = &clientKey
	}
	if *cred.serverCert == "" {
		cred.serverCert = &serverCert
	}
}


func (cred *authCredsType) initServerTrust() error {

	trustedCerts := *x509.NewCertPool()
	CertPoolAppendCertsFromFile(&trustedCerts, *cred.serverCert)

	if len(trustedCerts.Subjects()) == 0 {
		return errors.New("No server certificate is trusted," +
			" use -trusted-certs with a proper certificate")
	}
  cred.trustedCerts = trustedCerts
	return nil
}

func (cred *authCredsType) loadClientCert() error {
	if clientCert, err := tls.LoadX509KeyPair(*cred.certFile, *cred.certKey); err != nil {
		return errors.New("Failed to load certificate and key from files: " +
			*cred.certFile + " " + *cred.certKey)
	} else {
		cred.clientCert = clientCert
	}
	return nil
}

func initClientAndServerAuthCreds(authCreds *authCredsType) error {

	if *authCreds.bootstrapServer == "" {
		panic("trying to validate bootstrap parameters while not performing bootstrap")
	}

	// set default values if nothing is provided via command line
	authCreds.setDefaultKeysAndCerts(defaultCertFile, defaultCertKey, defaultServerCert)
  if err := authCreds.initServerTrust(); err != nil {
		return err
	}
	if err := authCreds.loadClientCert(); err != nil {
		return err
	}

	return nil
}

func initClient(trustedCerts x509.CertPool, clientCert tls.Certificate) *http.Client {

	tlsConf := tls.Config{
		RootCAs:      &trustedCerts,
		Certificates: []tls.Certificate{clientCert},
		// InsecureSkipVerify : true,
	}

	transport := http.Transport{
		TLSClientConfig: &tlsConf,
	}

	return &http.Client{
		Transport: &transport,
	}
}

func (c *Client) doBootstrap() error {

	serverURL := c.BaseURL + "/bootstrap"
	log.Error("Sending HTTP GET to: ", serverURL)

	response, err := c.HTTPClient.Get(serverURL)
	if err != nil {
		return err
	}
	defer response.Body.Close()

	log.Error("Received headers:", response.Header)

	if respData, err := ioutil.ReadAll(response.Body); err != nil {
		return err
	} else {
		log.Error("Received data:", string(respData))
	}

	return nil
}
