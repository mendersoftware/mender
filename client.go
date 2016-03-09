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

//TODO: this will be hardcoded for now but should be configurable in future
const (
	defaultCertFile   = "/data/certfile.crt"
	defaultCertKey    = "/data/certkey.key"
	defaultServerCert = "/data/server.crt"
)

type authCmdLineArgsType struct {
	// hostname or address to bootstrap to
	bootstrapServer string
	certFile        string
	certKey         string
	serverCert      string
}

func (cred *authCmdLineArgsType) setDefaultKeysAndCerts(clientCert, clientKey,
	serverCert string) {
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

// Client represents the http(s) client used for network communication.
//
type Client struct {
	BaseURL string
	authCredsType
	HTTPClient *http.Client
}

// Client initialization

// NewClient returnes a poiter to newly initialized client
func NewClient(args authCmdLineArgsType) (*Client, error) {
	var httpsClient Client
	args.setDefaultKeysAndCerts(defaultCertFile, defaultCertKey, defaultServerCert)

	if err := httpsClient.initServerTrust(args); err != nil {
		return nil, err
	}
	if err := httpsClient.initClientCert(args); err != nil {
		return nil, err
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

	return &httpsClient, nil
}

func (c *Client) initServerTrust(args authCmdLineArgsType) error {

	if args.serverCert == "" {
		return errors.New("Can not read server certificate")
	}
	trustedCerts := *x509.NewCertPool()
	certPoolAppendCertsFromFile(&trustedCerts, args.serverCert)

	if len(trustedCerts.Subjects()) == 0 {
		return errorAddingServerCertificateToPool
	}
	c.trustedCerts = trustedCerts
	return nil
}

func (c *Client) initClientCert(args authCmdLineArgsType) error {
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

// Client request sending and parsing

type clientRequestType struct {
	reqType string
	request string
}

type clientRequester interface {
	formatRequest() clientRequestType
	actOnResponse(http.Response, []byte) error
	getClient() Client
}

func makeJobDone(req clientRequester) error {
	request := req.formatRequest()
	client := req.getClient()

	response, err := client.sendRequest(request.reqType, request.request)
	if err != nil {
		return err
	}

	defer response.Body.Close()

	respBody, err := ioutil.ReadAll(response.Body)
	if err != nil {
		return err
	}
	log.Debug("Received response body: ", string(respBody))

	return req.actOnResponse(*response, respBody)
}

func (c *Client) sendRequest(reqType string, request string) (*http.Response, error) {

	switch reqType {
	//TODO: in future we can use different request types
	case http.MethodGet:
		log.Debug("Sending HTTP GET: ", request)

		response, err := c.HTTPClient.Get(request)
		if err != nil {
			return nil, err
		}

		log.Debug("Received headers:", response.Header)
		log.Debug("Received response: ", response.Status)

		return response, nil
	}
	return nil, errors.New("trying to send unsupported request of type " + reqType)
}
