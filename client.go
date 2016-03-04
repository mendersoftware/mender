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

type Client struct {
	BaseURL    string
	HTTPClient *http.Client
}

type authCmdLineArgsType struct {
	// hostname or address to bootstrap to
	bootstrapServer string
	certFile        string
	certKey         string
	serverCert      string
}

func (cred *authCmdLineArgsType) setDefaultKeysAndCerts(clientCert string, clientKey string, serverCert string) {
	if cred.certFile == "" {
		cred.certFile = clientCert
	}
	if cred.certKey == "" {
		cred.certKey = clientKey
	}
	if cred.serverCert == "" {
		cred.serverCert = serverCert
	}
}

type authCredsType struct {
	// Cert+privkey that authenticates this client
	clientCert tls.Certificate
	// Trusted server certificates
	trustedCerts x509.CertPool
}

func (auth *authCredsType) initServerTrust(cred *authCmdLineArgsType) error {

	trustedCerts := *x509.NewCertPool()
	CertPoolAppendCertsFromFile(&trustedCerts, cred.serverCert)

	if len(trustedCerts.Subjects()) == 0 {
		return errors.New("No server certificate is trusted," +
			" use -trusted-certs with a proper certificate")
	}

	auth.trustedCerts = trustedCerts
	return nil
}

func (auth *authCredsType) initClientCert(cred *authCmdLineArgsType) error {
	if clientCert, err := tls.LoadX509KeyPair(cred.certFile, cred.certKey); err != nil {
		return errors.New("Failed to load certificate and key from files: " +
			cred.certFile + " " + cred.certKey)
	} else {
		auth.clientCert = clientCert
		return nil
	}
}

func initClientAndServerAuthCreds(cred *authCmdLineArgsType) (error, authCredsType) {

	var auth authCredsType

	if err := auth.initServerTrust(cred); err != nil {
		return err, (authCredsType{})
	}
	if err := auth.initClientCert(cred); err != nil {
		return err, (authCredsType{})
	}

	return nil, auth
}

func initClient(auth authCredsType) *http.Client {
	tlsConf := tls.Config{
		RootCAs:      &auth.trustedCerts,
		Certificates: []tls.Certificate{auth.clientCert},
	}

	transport := http.Transport{
		TLSClientConfig: &tlsConf,
	}

	return &http.Client{
		Transport: &transport,
	}
}

type httpReqType int

const (
	GET httpReqType = 1 + iota
	PUT
	POST
)

func (c *Client) sendRequest(reqType httpReqType, request string) (error, *http.Response) {

	switch reqType {
	case GET:
		log.Debug("Sending HTTP GET: ", request)

		response, err := c.HTTPClient.Get(request)
		if err != nil {
			return err, nil
		}
		defer response.Body.Close()

		log.Debug("Received headers:", response.Header)

		if respData, err := ioutil.ReadAll(response.Body); err != nil {
			return err, nil
		} else {
			log.Debug("Received data:", string(respData))
		}
		return nil, response

	case PUT:
		//TODO:
		panic("PUT not implemented yet")
	case POST:
		//TODO:
		panic("POST not implemented yet")
	}
	panic("unknown http request")
}

func (c *Client) parseUpdateTesponse(response *http.Response) error {
	// TODO: do something with the stuff received
	log.Error("Received data:", response.Status)
	return nil
}
