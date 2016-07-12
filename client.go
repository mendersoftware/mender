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
	"fmt"
	"io/ioutil"
	"net/http"
	"strings"

	"github.com/mendersoftware/log"
	"github.com/pkg/errors"
)

const (
	apiPrefix = "/api/devices/0.1/"
)

var (
	errorLoadingClientCertificate      = errors.New("Failed to load certificate and key")
	errorAddingServerCertificateToPool = errors.New("Error adding trusted server certificate to pool.")
)

// Mender API Client wrapper. A standard http.Client is compatible with this
// interface and can be used without further configuration where ApiRequester is
// expected. Instead of instantiating the client by yourself, one can also use a
// wrapper call NewApiClient() that sets up TLS handling according to passed
// configuration.
type ApiRequester interface {
	Do(req *http.Request) (*http.Response, error)
}

type RequestProcessingFunc func(response *http.Response) (interface{}, error)

// wrapper for http.Client with additional methods
type ApiClient struct {
	http.Client
}

// Return a new ApiRequest sharing this ApiClient helper
func (a *ApiClient) Request(code AuthToken) *ApiRequest {
	return &ApiRequest{
		api:  a,
		auth: code,
	}
}

// ApiRequester compatible helper. The helper can be used for executing API
// requests that require authorization as provided Do() method will automatically
// setup authorization information in the request.
type ApiRequest struct {
	api *ApiClient
	// authorization code to use for requests
	auth AuthToken
}

func (ar *ApiRequest) Do(req *http.Request) (*http.Response, error) {
	if req.Header.Get("Authorization") == "" {
		req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", ar.auth))
	}
	return ar.api.Do(req)
}

// Client initialization
func NewApiClient(conf httpsClientConfig) (*ApiClient, error) {

	var client *http.Client
	if conf == (httpsClientConfig{}) {
		client = newHttpClient()
	} else {
		var err error
		client, err = newHttpsClient(conf)
		if err != nil {
			return nil, err
		}
	}

	return &ApiClient{*client}, nil
}

func newHttpClient() *http.Client {
	return &http.Client{}
}

func newHttpsClient(conf httpsClientConfig) (*http.Client, error) {
	client := newHttpClient()

	trustedcerts, err := loadServerTrust(conf)
	if err != nil {
		return nil, errors.Wrapf(err, "cannot initialize server trust")
	}

	clientcerts, err := loadClientCert(conf)
	if err != nil {
		return nil, errors.Wrapf(err, "can not load client certificate")
	}

	if conf.noVerify {
		log.Warnf("certificate verification skipped..")
	}
	tlsc := tls.Config{
		RootCAs:            trustedcerts,
		InsecureSkipVerify: conf.noVerify,
	}
	transport := http.Transport{
		TLSClientConfig: &tlsc,
	}

	if clientcerts != nil {
		transport.TLSClientConfig.Certificates = []tls.Certificate{*clientcerts}
	}

	client.Transport = &transport
	return client, nil
}

// Client configuration

type httpsClientConfig struct {
	certFile   string
	certKey    string
	serverCert string
	isHttps    bool
	noVerify   bool
}

func loadServerTrust(conf httpsClientConfig) (*x509.CertPool, error) {
	if conf.serverCert == "" {
		// TODO: this is for pre-production version only to simplify tests.
		// Make sure to remove in production version.
		log.Warn("Server certificate not provided. Trusting all servers.")
		return nil, nil
	}

	certs := x509.NewCertPool()
	// Read certificate file.
	cacert, err := ioutil.ReadFile(conf.serverCert)
	if err != nil {
		return nil, err
	}
	certs.AppendCertsFromPEM(cacert)

	if len(certs.Subjects()) == 0 {
		return nil, errorAddingServerCertificateToPool
	}
	return certs, nil
}

func loadClientCert(conf httpsClientConfig) (*tls.Certificate, error) {
	if conf.certFile == "" || conf.certKey == "" {
		// TODO: this is for pre-production version only to simplify tests.
		// Make sure to remove in production version.
		log.Warn("No client key and certificate provided. Using system default.")
		return nil, nil
	}

	clientCert, err := tls.LoadX509KeyPair(conf.certFile, conf.certKey)
	if err != nil {
		return nil, errorLoadingClientCertificate
	}
	return &clientCert, nil
}

func buildURL(server string) string {
	if strings.HasPrefix(server, "https://") || strings.HasPrefix(server, "http://") {
		return server
	}
	return "https://" + server
}

func buildApiURL(server, url string) string {
	if strings.HasPrefix(url, "/") {
		url = url[1:]
	}
	return buildURL(server) + apiPrefix + url
}
