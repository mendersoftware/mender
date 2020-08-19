// Copyright 2020 Northern.tech AS
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
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"io/ioutil"

	"github.com/mendersoftware/openssl"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	"golang.org/x/net/http2"
)

const (
	apiPrefix = "/api/devices/v1/"

	errMissingServerCertF = "IGNORING ERROR: The client server-certificate can not be " +
		"loaded: (%s). The client will continue running, but may not be able to " +
		"communicate with the server. If this is not your intention please add a valid " +
		"server certificate"
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
	// to reading the body. This is to timeout long lasting connections.
	//
	// 4 hours should be enough to download a 2GB image file with the
	// average download speed ~1 mbps
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

// MenderServer is a placeholder for a full server definition used when
// multiple servers are given. The fields corresponds to the definitions
// given in MenderConfig.
type MenderServer struct {
	ServerURL string
	// TODO: Move all possible server specific configurations in
	//       MenderConfig over to this struct. (e.g. TenantToken?)
}

// APIError is an error type returned after receiving an error message from the
// server. It wraps a regular error with the request_id - and if
// the server returns an error message, this is also returned.
type APIError struct {
	error
	reqID        string
	serverErrMsg string
}

func NewAPIError(err error, resp *http.Response) *APIError {
	a := APIError{
		error: err,
		reqID: resp.Header.Get("request_id"),
	}

	if resp.StatusCode >= 400 && resp.StatusCode < 600 {
		a.serverErrMsg = unmarshalErrorMessage(resp.Body)
	}
	return &a
}

func (a *APIError) Error() string {

	err := fmt.Sprintf("(request_id: %s): %s", a.reqID, a.error.Error())

	if a.serverErrMsg != "" {
		return err + fmt.Sprintf(" server error message: %s", a.serverErrMsg)
	}

	return err

}

// Cause returns the underlying error, as
// an APIError is merely an error wrapper.
func (a *APIError) Cause() error {
	return a.error
}

type RequestProcessingFunc func(response *http.Response) (interface{}, error)

// wrapper for http.Client with additional methods
type ApiClient struct {
	http.Client
}

// function type for reauthorization closure (see func reauthorize@mender.go)
type ClientReauthorizeFunc func(string) (AuthToken, error)

// function type for setting server (in case of multiple fallover servers)
type ServerManagementFunc func() *MenderServer

// Return a new ApiRequest
func (a *ApiClient) Request(code AuthToken, nextServerIterator ServerManagementFunc, reauth ClientReauthorizeFunc) *ApiRequest {
	return &ApiRequest{
		api:                a,
		auth:               code,
		nextServerIterator: nextServerIterator,
		revoke:             reauth,
	}
}

// ApiRequester compatible helper. The helper can be used for executing API
// requests that require authorization as provided Do() method will automatically
// setup authorization information in the request.
type ApiRequest struct {
	api *ApiClient
	// authorization code to use for requests
	auth AuthToken
	// anonymous function to initiate reauthorization
	revoke ClientReauthorizeFunc
	// anonymous function to set server
	nextServerIterator ServerManagementFunc
}

// tryDo is a wrapper around http.Do that also tries to reauthorize
// on a 401 response (Unauthorized).
func (ar *ApiRequest) tryDo(req *http.Request, serverURL string) (*http.Response, error) {
	r, err := ar.api.Do(req)
	if err == nil && r.StatusCode == http.StatusUnauthorized {
		// invalid JWT; most likely the token is expired:
		// Try to refresh it and reattempt sending the request
		log.Info("Device unauthorized; attempting reauthorization")
		if jwt, e := ar.revoke(serverURL); e == nil {
			// retry API request with new JWT token
			ar.auth = jwt
			// check if request had a body
			// (GetBody is optional, and nil if body is empty)
			if req.GetBody != nil {
				if body, e := req.GetBody(); e == nil {
					req.Body = body
				}
			}
			req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", ar.auth))
			r, err = ar.api.Do(req)
		} else {
			log.Warnf("Reauthorization failed with error: %s", e.Error())
		}
	}
	return r, err
}

// Do is a wrapper for http.Do function for ApiRequests. This function in
// addition to calling http.Do handles client-server authorization header /
// reauthorization, as well as attempting failover servers (if given) whenever
// the server "refuse" to serve the request.
func (ar *ApiRequest) Do(req *http.Request) (*http.Response, error) {
	if ar.nextServerIterator == nil {
		return nil, errors.New("Empty server list!")
	}
	if req.Header.Get("Authorization") == "" {
		// Add JWT to header
		req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", ar.auth))
	}
	var r *http.Response
	var host string
	var err error

	server := ar.nextServerIterator()
	for {
		// Split host from URL
		tmp := strings.Split(server.ServerURL, "://")
		if len(tmp) == 1 {
			host = tmp[0]
		} else {
			// (len >= 2) should usually be 2
			host = tmp[1]
		}

		req.URL.Host = host
		req.Host = host
		r, err = ar.tryDo(req, server.ServerURL)
		if err == nil && r.StatusCode < 400 {
			break
		}
		prewHost := server.ServerURL
		if server = ar.nextServerIterator(); server == nil {
			break
		}
		log.Warnf("Server %q failed to serve request %q. Attempting %q",
			prewHost, req.URL.Path, server.ServerURL)
	}
	if server != nil {
		// reset server iterator
		for {
			if ar.nextServerIterator() == nil {
				break
			}
		}
	}
	return r, err
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
		client.Transport = &http.Transport{
			Proxy: http.ProxyFromEnvironment,
		}
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

func loadServerTrust(ctx *openssl.Ctx, conf *Config) (*openssl.Ctx, error) {
	// Trust CA's in the standard location (/etc/ssl/certs), and the
	// configured server certificate when building the certificate chain
	err := ctx.LoadVerifyLocations(conf.ServerCert, "/etc/ssl/certs/")
	if err != nil && strings.Contains(err.Error(), "No such file or directory"){
		log.Errorf(errMissingServerCertF, conf.ServerCert)
	}
	return ctx, err
}

func loadClientTrust(ctx *openssl.Ctx, conf *Config) (*openssl.Ctx, error) {

	if conf.HttpsClient == nil {
		return ctx, errors.New("Empty HttpsClient config given")

	}
	certFile := conf.HttpsClient.Certificate
	keyFile := conf.HttpsClient.Key

	certBytes, err := ioutil.ReadFile(certFile)
	if err != nil {
		return ctx, errors.Wrap(err, "Failed to read the certificate file for the HttpsClient")
	}

	certs := openssl.SplitPEM(certBytes)
	if len(certs) == 0 {
		return ctx, fmt.Errorf("No PEM certificate found in '%s'", certFile)
	}
	first, certs := certs[0], certs[1:]
	cert, err := openssl.LoadCertificateFromPEM(first)
	if err != nil {
		return ctx, err
	}

	err = ctx.UseCertificate(cert)
	if err != nil {
		return ctx, err
	}

	for _, pem := range certs {
		cert, err := openssl.LoadCertificateFromPEM(pem)
		if err != nil {
			return ctx, err
		}
		err = ctx.AddChainCertificate(cert)
		if err != nil {
			return ctx, err
		}
	}

	keyBytes, err := ioutil.ReadFile(keyFile)
	if err != nil {
		return ctx, errors.Wrap(err, "Private key file from the HttpsClient configuration not found")
	}

	key, err := openssl.LoadPrivateKeyFromPEM(keyBytes)
	if err != nil {
		return ctx, err
	}

	err = ctx.UsePrivateKey(key)
	if err != nil {
		return ctx, err
	}

	return ctx, nil
}

func dialOpenSSL(ctx *openssl.Ctx, conf *Config, network string, addr string) (net.Conn, error) {

	flags := openssl.DialFlags(0)

	if conf.NoVerify {
		flags = openssl.InsecureSkipHostVerification
	}

	conn, err := openssl.Dial("tcp", addr, ctx, flags)
	if err != nil {
		return nil, err
	}

	v := conn.VerifyResult()
	if v != openssl.Ok {
		if v == openssl.CertHasExpired {
			return nil, errors.Errorf("certificate has expired, "+
				"openssl verify rc: %d server cert file: %s", v, conf.ServerCert)
		}
		if v == openssl.DepthZeroSelfSignedCert {
			return nil, errors.Errorf("depth zero self-signed certificate, "+
				"openssl verify rc: %d server cert file: %s", v, conf.ServerCert)
		}
		if v == openssl.EndEntityKeyTooSmall {
			return nil, errors.Errorf("end entity key too short, "+
				"openssl verify rc: %d server cert file: %s", v, conf.ServerCert)
		}
		if v == openssl.UnableToGetIssuerCertLocally {
			return nil, errors.Errorf("certificate signed by unknown authority, "+
				"openssl verify rc: %d server cert file: %s", v, conf.ServerCert)
		}
		return nil, errors.Errorf("not a valid certificate, "+
			"openssl verify rc: %d server cert file: %s", v, conf.ServerCert)
	}
	return conn, err
}

func newHttpsClient(conf Config) (*http.Client, error) {
	client := newHttpClient()

	ctx, err := openssl.NewCtx()
	if err != nil {
		return nil, err
	}

	ctx, err = loadServerTrust(ctx, &conf)
	if err != nil {
		log.Warn("Failed to load the server TLS certificate settings")
	}

	if conf.HttpsClient != nil {
		ctx, err = loadClientTrust(ctx, &conf)
		if err != nil {
			log.Warn("Failed to load the client TLS certificate settings")
		}
	}

	if conf.NoVerify {
		log.Warnf("certificate verification skipped..")
	}
	transport := http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialTLS: func(network string, addr string) (net.Conn, error) {
			return dialOpenSSL(ctx, &conf, network, addr)
		},
	}

	client.Transport = &transport
	return client, nil
}

// Client configuration

// HttpsClient holds the configuration for the client side mTLS configuration
type HttpsClient struct {
	Certificate string
	Key         string
}

func (h *HttpsClient) Validate() {
	if h == nil {
		return
	}
	// Check if both the 'ServerCertificate' and 'PrivateKey' fields are set
	if h.Certificate != "" || h.Key != "" {
		if h.Certificate == "" {
			log.Error("The 'PrivateKey' field is set in the mTLS configuration, but no 'ServerCertificate' is given. Both need to be present in order for mTLS to function")
		}
		if h.Key == "" {
			log.Error("The 'ServerCertificate' field is set in the mTLS configuration, but no 'PrivateKey' is given. Both need to be present in order for mTLS to function")
		}
	}
}

type Config struct {
	IsHttps    bool
	ServerCert string
	*HttpsClient
	NoVerify bool
}

func buildURL(server string) string {
	if strings.HasPrefix(server, "https://") || strings.HasPrefix(server, "http://") {
		return server
	}
	return "https://" + server
}

func buildApiURL(server, url string) string {
	return buildURL(server) + apiPrefix + strings.TrimPrefix(url, "/")
}

// Normally one minute, but used in tests to lower the interval to avoid
// waiting.
var ExponentialBackoffSmallestUnit time.Duration = time.Minute

// Simple algorithm: Start with one minute, and try three times, then double
// interval (maxInterval is maximum) and try again. Repeat until we tried
// three times with maxInterval.
func GetExponentialBackoffTime(tried int, maxInterval time.Duration) (time.Duration, error) {
	const perIntervalAttempts = 3

	interval := 1 * ExponentialBackoffSmallestUnit
	nextInterval := interval

	for c := 0; c <= tried; c += perIntervalAttempts {
		interval = nextInterval
		nextInterval *= 2
		if interval >= maxInterval {
			if tried-c >= perIntervalAttempts {
				// At max interval and already tried three
				// times. Give up.
				return 0, errors.New("Tried maximum amount of times")
			}

			// Don't use less than the smallest unit, usually one
			// minute.
			if maxInterval < ExponentialBackoffSmallestUnit {
				return ExponentialBackoffSmallestUnit, nil
			}
			return maxInterval, nil
		}
	}

	return interval, nil
}

// unmarshalErrorMessage unmarshals the error message contained in an
// error request from the server.
func unmarshalErrorMessage(r io.Reader) string {
	e := new(struct {
		Error string `json:"error"`
	})
	if err := json.NewDecoder(r).Decode(e); err != nil {
		return fmt.Sprintf("failed to parse server response: %v", err)
	}
	return e.Error
}
