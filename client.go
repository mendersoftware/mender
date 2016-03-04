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

type client struct {
	BaseURL    string
	HTTPClient *http.Client
}

func certPoolAppendCertsFromFile(s *x509.CertPool, f string) bool {
	cacert, err := ioutil.ReadFile(f)
	if err != nil {
		log.Warnln("Error reading certificate file ", err)
		return false
	}

	ret := s.AppendCertsFromPEM(cacert)
	return ret
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

func (auth *authCredsType) initServerTrust(cred authCmdLineArgsType) error {

	trustedCerts := *x509.NewCertPool()
	certPoolAppendCertsFromFile(&trustedCerts, cred.serverCert)

	if len(trustedCerts.Subjects()) == 0 {
		return errors.New("No server certificate is trusted," +
			" use -trusted-certs with a proper certificate")
	}

	auth.trustedCerts = trustedCerts
	return nil
}

func (auth *authCredsType) initClientCert(cred authCmdLineArgsType) error {
	clientCert, err := tls.LoadX509KeyPair(cred.certFile, cred.certKey)
	if err != nil {
		return errors.New("Failed to load certificate and key from files: " +
			cred.certFile + " " + cred.certKey)
	}
	auth.clientCert = clientCert
	return nil

}

func initClientAndServerAuthCreds(cred authCmdLineArgsType) (authCredsType, error) {

	var auth authCredsType

	if err := auth.initServerTrust(cred); err != nil {
		return authCredsType{}, err
	}
	if err := auth.initClientCert(cred); err != nil {
		return authCredsType{}, err
	}

	return auth, nil
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

func (c *client) sendRequest(reqType httpReqType, request string) (*http.Response, error) {

	switch reqType {
	case GET:
		log.Debug("Sending HTTP GET: ", request)

		response, err := c.HTTPClient.Get(request)
		if err != nil {
			return nil, err
		}
		defer response.Body.Close()

		log.Debug("Received headers:", response.Header)

		respData, err := ioutil.ReadAll(response.Body)
		if err != nil {
			return nil, err
		}

		log.Debug("Received data:", string(respData))
		return response, nil

	case PUT:
		//TODO:
		panic("PUT not implemented yet")
	case POST:
		//TODO:
		panic("POST not implemented yet")
	}
	panic("unknown http request")
}

func (c *client) parseUpdateTesponse(response *http.Response) error {
	// TODO: do something with the stuff received
	log.Error("Received data:", response.Status)
	return nil
}
