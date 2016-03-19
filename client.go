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
	"io"
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
	defaultCertFile         = "/data/certfile.crt"
	defaultCertKey          = "/data/certkey.key"
	defaultServerCert       = "/data/server.crt"
	minimumImageSize  int64 = 4096
)

type RequestProcessingFunc func(response *http.Response) (interface{}, error)

type Updater interface {
	GetScheduledUpdate(RequestProcessingFunc, string) (interface{}, error)
	FetchUpdate(string) (io.ReadCloser, int64, error)
}

// Client represents the http(s) client used for network communication.
//
type client struct {
	authCredsType
	HTTPClient   *http.Client
	minImageSize int64
}

// Client initialization

func NewClient(args authCmdLineArgsType) *client {
	var httpsClient client
	httpsClient.minImageSize = minimumImageSize
	args.setDefaultKeysAndCerts(defaultCertFile, defaultCertKey, defaultServerCert)

	if err := httpsClient.initServerTrust(args); err != nil {
		return nil
	}
	if err := httpsClient.initClientCert(args); err != nil {
		return nil
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

	return &httpsClient
}

func (c *client) GetScheduledUpdate(process RequestProcessingFunc, server string) (interface{}, error) {
	r, err := c.makeAndSendRequest(http.MethodGet, server)
	if err != nil {
		return nil, err
	}

	defer r.Body.Close()

	return process(r)
}

// Returns a byte stream which is a download of the given link.
func (c *client) FetchUpdate(url string) (io.ReadCloser, int64, error) {
	r, err := c.makeAndSendRequest(http.MethodGet, url)
	if err != nil {
		return nil, -1, err
	}

	if r.ContentLength < 0 {
		return nil, -1, errors.New("Will not continue with unknown image size.")
	} else if r.ContentLength < c.minImageSize {
		return nil, -1, errors.New("Less than " + string(c.minImageSize) + "KiB image update (" +
			string(r.ContentLength) + " bytes)? Something is wrong, aborting.")
	}

	return r.Body, r.ContentLength, nil
}

func (client *client) makeAndSendRequest(method, url string) (*http.Response, error) {

	res, err := http.NewRequest(method, url, nil)
	if err != nil {
		return nil, err
	}

	log.Debug("Sending HTTP [", method, "] request: ", url)
	return client.HTTPClient.Do(res)
}

// possible API responses received for update request
const (
	updateResponseHaveUpdate = 200
	updateResponseNoUpdates  = 204
	updateResponseError      = 404
)

// have update for the client
type UpdateResponse struct {
	Image struct {
		URI      string
		Checksum string
		ID       string
	}
	ID string
}

func validateGetUpdate(update UpdateResponse) error {
	// check if we have JSON data correctky decoded
	if update.ID != "" && update.Image.ID != "" && update.Image.Checksum != "" && update.Image.URI != "" {
		log.Info("Received correct request for getting image from: " + update.Image.URI)
		return nil
	}
	return errors.New("Missing parameters in encoded JSON response")
}

func processUpdateResponse(response *http.Response) (interface{}, error) {
	log.Debug("Received response:", response.Status)

	respBody, err := ioutil.ReadAll(response.Body)
	if err != nil {
		return nil, err
	}

	switch response.StatusCode {
	case updateResponseHaveUpdate:
		log.Debug("Have update available")

		data := new(UpdateResponse)
		if err := json.Unmarshal(respBody, data); err != nil {
			switch err.(type) {
			case *json.SyntaxError:
				return nil, errors.New("Error parsing data syntax")
			}
			return nil, errors.New("Error parsing data: " + err.Error())
		}
		if err := validateGetUpdate(*data); err != nil {
			return nil, err
		}
		return data, nil

	case updateResponseNoUpdates:
		log.Debug("No update available")
		return nil, nil

	case updateResponseError:
		return nil, errors.New("Client not authorized to get update schedule.")

	default:
		return nil, errors.New("Invalid response received from server")
	}
}

// Client configuration

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

func (c *client) initServerTrust(args authCmdLineArgsType) error {

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
