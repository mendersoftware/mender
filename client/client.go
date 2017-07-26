// Copyright 2017 Northern.tech AS
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
package client

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/mendersoftware/log"
	"github.com/pkg/errors"
	"golang.org/x/net/http2"
)

const (
	apiPrefix = "/api/devices/v1/"
)

var (
	errorAddingServerCertificateToPool = errors.New("Error adding trusted server certificate to pool.")
)

var (
	// 	                  http.Client.Timeout
	// +--------------------------------------------------------+
	// +--------+  +---------+  +-------+  +--------+  +--------+
	// |  Dial  |  |   TLS   |  |Request|  |Response|  |Response|
	// |        |  |handshake|  |       |  |headers |  |body    |
	// +--------+  +---------+  +-------+  +--------+  +--------+
	// +--------+  +---------+             +--------+
	//  Dial        TLS                     Response
	//  timeout     handshake               header
	//  timeout                             timeout
	//
	//  It covers the entire exchange, from Dial (if a connection is not reused)
	// to reading the body. This is to timeout long lasing connections.
	//
	// 4 hours shold be enough to download 2GB image file with the
	// average download spead ~1 mbps
	defaultClientReadingTimeout = 4 * time.Hour

	// connection keepalive options
	connectionKeepaliveTime = 10 * time.Second
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

func NewApiClient(conf Config) (*ApiClient, error) {
	return New(conf)
}

// New initializes new client
func New(conf Config) (*ApiClient, error) {

	var client *http.Client
	if conf == (Config{}) {
		client = newHttpClient()
	} else {
		var err error
		client, err = newHttpsClient(conf)
		if err != nil {
			return nil, err
		}
	}

	if client.Transport == nil {
		client.Transport = &http.Transport{}
	}
	// set connection timeout
	client.Timeout = defaultClientReadingTimeout

	transport := client.Transport.(*http.Transport)
	//set keepalive options
	transport.DialContext = (&net.Dialer{
		KeepAlive: connectionKeepaliveTime,
	}).DialContext

	if err := http2.ConfigureTransport(transport); err != nil {
		log.Warnf("failed to enable HTTP/2 for client: %v", err)
	}

	return &ApiClient{*client}, nil
}

func newHttpClient() *http.Client {
	return &http.Client{}
}

func newHttpsClient(conf Config) (*http.Client, error) {
	client := newHttpClient()

	trustedcerts, err := loadServerTrust(conf)
	if err != nil {
		return nil, errors.Wrapf(err, "cannot initialize server trust")
	}

	if conf.NoVerify {
		log.Warnf("certificate verification skipped..")
	}
	tlsc := tls.Config{
		RootCAs:            trustedcerts,
		InsecureSkipVerify: conf.NoVerify,
	}
	transport := http.Transport{
		TLSClientConfig: &tlsc,
	}

	client.Transport = &transport
	return client, nil
}

// Client configuration

type Config struct {
	ServerCert string
	IsHttps    bool
	NoVerify   bool
}

func loadServerTrust(conf Config) (*x509.CertPool, error) {
	if conf.ServerCert == "" {
		// TODO: this is for pre-production version only to simplify tests.
		// Make sure to remove in production version.
		log.Warn("Server certificate not provided. Trusting all servers.")
		return nil, nil
	}

	certs, err := x509.SystemCertPool()
	if err != nil {
		return nil, err
	}

	// Read certificate file.
	cacert, err := ioutil.ReadFile(conf.ServerCert)
	if err != nil {
		return nil, err
	}
	certs.AppendCertsFromPEM(cacert)

	if len(certs.Subjects()) == 0 {
		return nil, errorAddingServerCertificateToPool
	}
	return certs, nil
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
