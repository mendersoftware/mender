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
	"strings"

	"github.com/mendersoftware/log"
)

var (
	errorLoadingClientCertificate      = errors.New("Failed to load certificate and key")
	errorNoServerCertificateFound      = errors.New("No server certificate is provided, use -trusted-certs with a proper certificate.")
	errorAddingServerCertificateToPool = errors.New("Error adding trusted server certificate to pool.")
)

const (
	minimumImageSize int64 = 4096 //kB
)

type RequestProcessingFunc func(response *http.Response) (interface{}, error)

type Updater interface {
	GetScheduledUpdate(processFunc RequestProcessingFunc, server string, deviceID string) (interface{}, error)
	FetchUpdate(url string) (io.ReadCloser, int64, error)
}

// Client represents the http(s) client used for network communication.
//
type httpClient struct {
	HTTPClient   *http.Client
	minImageSize int64
}

type httpsClient struct {
	httpClient
	httpsClientAuthCreds
}

// Client initialization

func NewUpdater(conf httpsClientConfig) (Updater, error) {

	if conf == (httpsClientConfig{}) {
		client := NewHttpClient()
		if client == nil {
			return nil, errors.New("Can not instantialte updater.")
		}
		return client, nil
	}
	client := NewHttpsClient(conf)
	if client == nil {
		return nil, errors.New("Can not instantialte updater.")
	}
	return client, nil
}

func NewHttpClient() *httpClient {
	var client httpClient
	client.minImageSize = minimumImageSize
	client.HTTPClient = &http.Client{}

	return &client
}

func NewHttpsClient(conf httpsClientConfig) *httpsClient {
	var client httpsClient
	client.httpClient = *NewHttpClient()

	if err := client.initServerTrust(conf); err != nil {
		log.Error("Can not initialize server trust: ", err)
		return nil
	}

	if err := client.initClientCert(conf); err != nil {
		log.Error("Can not initialize client certificate: ", err)
		return nil
	}

	transport := http.Transport{
		TLSClientConfig: &tls.Config{
			RootCAs:      &client.trustedCerts,
			Certificates: []tls.Certificate{client.clientCert},
		},
	}

	client.HTTPClient.Transport = &transport
	return &client
}

// Client configuration

type httpsClientConfig struct {
	certFile   string
	certKey    string
	serverCert string
	isHttps    bool
}

type httpsClientAuthCreds struct {
	// Cert+privkey that authenticates this client
	clientCert tls.Certificate
	// Trusted server certificates
	trustedCerts x509.CertPool
}

func (c *httpsClient) initServerTrust(conf httpsClientConfig) error {
	if conf.serverCert == "" {
		// TODO: this is for pre-production version only to simplify tests.
		// Make sure to remove in production version.
		log.Warn("Server certificate not provided. Trusting all servers.")
		return nil
	}

	c.trustedCerts = *x509.NewCertPool()
	// Read certificate file.
	cacert, err := ioutil.ReadFile(conf.serverCert)
	if err != nil {
		return err
	}
	c.trustedCerts.AppendCertsFromPEM(cacert)

	if len(c.trustedCerts.Subjects()) == 0 {
		return errorAddingServerCertificateToPool
	}
	return nil
}

func (c *httpsClient) initClientCert(conf httpsClientConfig) error {
	if conf.certFile == "" || conf.certKey == "" {
		// TODO: this is for pre-production version only to simplify tests.
		// Make sure to remove in production version.
		log.Warn("No client key and certificate provided. Using system default.")
		return nil
	}

	clientCert, err := tls.LoadX509KeyPair(conf.certFile, conf.certKey)
	if err != nil {
		return errorLoadingClientCertificate
	}
	c.clientCert = clientCert
	return nil
}

func (c *httpClient) GetScheduledUpdate(process RequestProcessingFunc,
	server string, deviceID string) (interface{}, error) {
	// http client should be able to perform https requests
	if strings.HasPrefix(server, "http://") || strings.HasPrefix(server, "https://") {
		return c.getUpdateInfo(process, server, deviceID)
	}
	return c.getUpdateInfo(process, "http://"+server, deviceID)
}

func (c *httpsClient) GetScheduledUpdate(process RequestProcessingFunc,
	server string, deviceID string) (interface{}, error) {
	if strings.HasPrefix(server, "https://") {
		return c.getUpdateInfo(process, server, deviceID)
	}
	return c.getUpdateInfo(process, "https://"+server, deviceID)
}

func (c *httpClient) getUpdateInfo(process RequestProcessingFunc, server string,
	deviceID string) (interface{}, error) {
	request := server + "/api/0.0.1/devices/" + deviceID + "/update"
	r, err := c.makeAndSendRequest(http.MethodGet, request)
	if err != nil {
		log.Debug("Sending request error: ", err)
		return nil, err
	}

	defer r.Body.Close()

	return process(r)
}

// Returns a byte stream which is a download of the given link.
func (c *httpClient) FetchUpdate(url string) (io.ReadCloser, int64, error) {
	r, err := c.makeAndSendRequest(http.MethodGet, url)
	if err != nil {
		log.Error("Can not fetch update image: ", err)
		return nil, -1, err
	}

	log.Debugf("Received fetch update response %v+", r)

	if r.StatusCode != http.StatusOK {
		log.Errorf("Error fetching shcheduled update info: code (%d)", r.StatusCode)
		return nil, -1, errors.New("Error receiving scheduled update information.")
	}

	if r.ContentLength < 0 {
		return nil, -1, errors.New("Will not continue with unknown image size.")
	} else if r.ContentLength < c.minImageSize {
		log.Errorf("Image smaller than expected. Expected: %d, received: %d", c.minImageSize, r.ContentLength)
		return nil, -1, errors.New("Image size is smaller than expected. Aborting.")
	}

	return r.Body, r.ContentLength, nil
}

func (client *httpClient) makeAndSendRequest(method, url string) (*http.Response, error) {

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
		YoctoID  string `json:"yocto_id"`
		ID       string
	}
	ID string
}

func validateGetUpdate(update UpdateResponse) error {
	// check if we have JSON data correctky decoded
	if update.ID != "" && update.Image.ID != "" &&
		update.Image.URI != "" && update.Image.YoctoID != "" {
		log.Info("Correct request for getting image from: " + update.Image.URI)
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

		data := UpdateResponse{}
		if err := json.Unmarshal(respBody, &data); err != nil {
			switch err.(type) {
			case *json.SyntaxError:
				return nil, errors.New("Error parsing data syntax")
			}
			return nil, errors.New("Error parsing data: " + err.Error())
		}
		if err := validateGetUpdate(data); err != nil {
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
