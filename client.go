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

type authCredsType struct {
	// Cert+privkey that authenticates this client
	clientCert tls.Certificate
	// Trusted server certificates
	trustedCerts x509.CertPool
}

type client struct {
	BaseURL string
	authCredsType
	HTTPClient *http.Client
}

func initClient(args authCmdLineArgsType) (client, error) {
	var httpsClient client
	args.setDefaultKeysAndCerts(defaultCertFile, defaultCertKey, defaultServerCert)

	if err := httpsClient.initServerTrust(args); err != nil {
		return client{}, err
	}
	if err := httpsClient.initClientCert(args); err != nil {
		return client{}, err
	}

	tlsConf := tls.Config{
		RootCAs:      &httpsClient.trustedCerts,
		Certificates: []tls.Certificate{httpsClient.clientCert},
	}

	transport := http.Transport{
		TLSClientConfig: &tlsConf,
	}

	httpsClient.HTTPClient = &http.Client{
		Transport: &transport,
	}

	return httpsClient, nil
}

func (c *client) initServerTrust(args authCmdLineArgsType) error {

	if args.serverCert == "" {
		return errorNoServerCertificateFound
	}
	trustedCerts := *x509.NewCertPool()
	certPoolAppendCertsFromFile(&trustedCerts, args.serverCert)

	if len(trustedCerts.Subjects()) == 0 {
		return errorAddingServerCertificateToPool
	}
	c.trustedCerts = trustedCerts
	return nil
}

func (c *client) initClientCert(args authCmdLineArgsType) error {
	clientCert, err := tls.LoadX509KeyPair(args.certFile, args.certKey)
	if err != nil {
		return errorLoadingClientCertificate
	}
	c.clientCert = clientCert
	return nil
}

func certPoolAppendCertsFromFile(s *x509.CertPool, f string) bool {
	cacert, err := ioutil.ReadFile(f)
	if err != nil {
		log.Warnln("Error reading certificate file ", err)
		return false
	}

	return s.AppendCertsFromPEM(cacert)
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
