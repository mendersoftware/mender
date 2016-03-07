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

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"io/ioutil"
	"net/http"

	"github.com/mendersoftware/log"
)

var (
	errorLoadingClientCertificate = errors.New("Failed to load certificate and key")
	errorNoServerCertificateFound = errors.New("No server certificate is provided," +
		" use -trusted-certs with a proper certificate.")
	errorAddingServerCertificateToPool = errors.New("Adding server certificate " +
		"to trusted pool failed.")
)

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

	return s.AppendCertsFromPEM(cacert)
}

type authCmdLineArgsType struct {
	// hostname or address to bootstrap to
	bootstrapServer string
	certFile        string
	certKey         string
	serverCert      string
}

func (cred *authCmdLineArgsType) setDefaultKeysAndCerts(clientCert, clientKey, serverCert string) {
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

	if cred.serverCert == "" {
		return errorNoServerCertificateFound
	}
	trustedCerts := *x509.NewCertPool()
	certPoolAppendCertsFromFile(&trustedCerts, cred.serverCert)

	if len(trustedCerts.Subjects()) == 0 {
		return errorAddingServerCertificateToPool
	}

	auth.trustedCerts = trustedCerts
	return nil
}

func (auth *authCredsType) initClientCert(cred authCmdLineArgsType) error {
	clientCert, err := tls.LoadX509KeyPair(cred.certFile, cred.certKey)
	if err != nil {
		return errorLoadingClientCertificate
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

const (
	updateRespponseHaveUpdate = 200
	updateResponseNoUpdates   = 204
	updateResponseError       = 404
)

func (c *client) sendRequest(reqType string, request string) (*http.Response, error) {

	switch reqType {
	//TODO: in future we can use different request types
	case http.MethodGet:
		log.Debug("Sending HTTP GET: ", request)

		response, err := c.HTTPClient.Get(request)
		if err != nil {
			return nil, err
		}
		//defer response.Body.Close()

		log.Debug("Received headers:", response.Header)
		return response, nil
	}
	panic("unknown http request")
}

type responseType interface {
}

type updateAPIResponseType struct {
	Image struct {
		URI       string
		Chaecksum string
		ID        string
	}
	ID string
}

func (c *client) parseUpdateResponse(response *http.Response) error {
	// TODO: do something with the stuff received
	log.Debug("Received response:", response.Status)
	switch response.StatusCode {
	case updateRespponseHaveUpdate:
		log.Debug("Have update available")

		//dec := json.NewDecoder(response.Body)
		respData, err := ioutil.ReadAll(response.Body)
		if err != nil {
			return err
		}

		log.Error("Received response body: ", string(respData))

		var data updateAPIResponseType
		if err := json.Unmarshal(respData, &data); err != nil {
			log.Error("Error parsing data -> " + err.Error())
			switch err.(type) {
			case *json.SyntaxError:
				log.Error("Error parsing data syntax")
			}
		}

	case updateResponseNoUpdates:
		log.Debug("No update available")
	case updateResponseError:

	default:

	}

	return nil
}
