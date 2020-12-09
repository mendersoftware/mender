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
	"path"
	"strings"
	"time"

	"io/ioutil"

	"github.com/mendersoftware/openssl"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
)

const (
	apiPrefix = "/api/devices/v1/"

	errMissingServerCertF = "IGNORING ERROR: The client server-certificate can not be " +
		"loaded: (%s). The client will continue running, but may not be able to " +
		"communicate with the server. If this is not your intention please add a valid " +
		"server certificate"
	errMissingCerts = "No trusted certificates. The client will continue running, but will " +
		"not be able to communicate with the server. Either specify ServerCertificate in " +
		"mender.conf, or make sure that CA certificates are installed on the system"
	pkcs11URIPrefix = "pkcs11:"
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

		log.Debugf("Connecting to server %s", host)

		var body io.ReadCloser
		if req.GetBody != nil {
			body, err = req.GetBody()
			if err != nil {
				return nil, errors.Wrap(err, "Unable to reconstruct HTTP request body")
			}
		} else {
			body = nil
		}

		// create a new request object to avoid issues when consuming
		// the request body multiple times when failing over a different
		// server. It is not safe to reuse the same request multiple times
		// when request.Body is not nil
		//
		// see: https://github.com/golang/go/issues/19653
		// Error message: http: ContentLength=52 with Body length 0
		newReq, _ := http.NewRequest(req.Method, req.URL.String(), body)
		newReq.Header = req.Header
		newReq.GetBody = req.GetBody

		r, err = ar.tryDo(newReq, server.ServerURL)
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

	return &ApiClient{*client}, nil
}

func newHttpClient() *http.Client {
	return &http.Client{}
}

// nrOfSystemCertsFound simply returns the number of certificates found in
// 'certDir'. The only reason this is needed, is so that the user can be
// notified if the system certificate directory is empty, since this is not done
// by the OpenSSL wrapper, in the same manner it was done by the Go standard
// library. OpenSSL loads the trust chain on a dial. This system cert, and
// server cert path are set by the 'SetDefaultVerifyPaths' and
// 'LoadVerifyLocations' respectively.
func nrOfSystemCertsFound(certDir string) (int, error) {
	sysCertsFound := 0
	files, err := ioutil.ReadDir(certDir)
	if err != nil {
		return 0, fmt.Errorf("Failed to read the OpenSSL default directory (%s). Err %v", certDir, err.Error())
	}
	for _, certFile := range files {
		certBytes, err := ioutil.ReadFile(path.Join(certDir, certFile.Name()))
		if err != nil {
			log.Debugf("Failed to read the certificate file for the HttpsClient. Err %v", err.Error())
			continue
		}

		certs := openssl.SplitPEM(certBytes)
		if len(certs) == 0 {
			log.Debugf("No PEM certificate found in '%s'", certFile)
			continue
		}
		first, certs := certs[0], certs[1:]
		_, err = openssl.LoadCertificateFromPEM(first)
		if err != nil {
			log.Debug(err.Error())
		} else {
			sysCertsFound += 1
		}
	}
	return sysCertsFound, nil
}

func loadServerTrust(ctx *openssl.Ctx, conf *Config) (*openssl.Ctx, error) {
	defaultCertDir, err := openssl.GetDefaultCertificateDirectory()
	if err != nil {
		return ctx, errors.Wrap(err, "Failed to get the default OpenSSL certificate directory. Please verify the OpenSSL setup")
	}
	sysCertsFound, err := nrOfSystemCertsFound(defaultCertDir)
	if err != nil {
		log.Warnf("Failed to list the system certificates with error: %s", err.Error())
	}

	// Set the default system certificate path for this OpenSSL context
	err = ctx.SetDefaultVerifyPaths()
	if err != nil {
		return ctx, fmt.Errorf("Failed to set the default OpenSSL directory. OpenSSL error code: %s", err.Error())
	}
	// Load the server certificate into the OpenSSL context
	err = ctx.LoadVerifyLocations(conf.ServerCert, "")
	if err != nil {
		if strings.Contains(err.Error(), "No such file or directory") {
			log.Warnf(errMissingServerCertF, conf.ServerCert)
		} else {
			log.Errorf("Failed to Load the Server certificate. Err %s", err.Error())
		}
		// If no system certificates, nor a server certificate is found,
		// warn the user, as this is a pretty common error.
		if sysCertsFound == 0 {
			log.Error(errMissingCerts)
		}
	}
	return ctx, err
}

func loadPrivateKey(keyFile string, engineId string) (key openssl.PrivateKey, err error) {
	if strings.HasPrefix(keyFile, pkcs11URIPrefix) {
		engine, err := openssl.EngineById(engineId)
		if err != nil {
			log.Errorf("Failed to Load '%s' engine. Err %s",
				engineId, err.Error())
			return nil, err
		}

		key, err = openssl.EngineLoadPrivateKey(engine, keyFile)
		if err != nil {
			log.Errorf("Failed to Load private key from engine '%s'. Err %s",
				engineId, err.Error())
			return nil, err
		}
		log.Infof("loaded private key: '%s...' from '%s'.", pkcs11URIPrefix, engineId)
	} else {
		keyBytes, err := ioutil.ReadFile(keyFile)
		if err != nil {
			return nil, errors.Wrap(err, "Private key file from the HttpsClient configuration not found")
		}

		key, err = openssl.LoadPrivateKeyFromPEM(keyBytes)
		if err != nil {
			return nil, err
		}
	}

	return key, nil
}

func loadClientTrust(ctx *openssl.Ctx, conf *Config) (*openssl.Ctx, error) {

	if conf.HttpsClient == nil {
		return ctx, errors.New("Empty HttpsClient config given")

	}
	certFile := conf.HttpsClient.Certificate

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

	key, err := loadPrivateKey(conf.HttpsClient.Key, conf.HttpsClient.SSLEngine)
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
		log.Warn(errors.Wrap(err, "Failed to load the server TLS certificate settings"))
	}

	if conf.HttpsClient != nil {
		ctx, err = loadClientTrust(ctx, &conf)
		if err != nil {
			log.Warn(errors.Wrap(err, "Failed to load the client TLS certificate settings"))
		}
	}

	if conf.NoVerify {
		log.Warn("certificate verification skipped..")
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
// NOTE: Careful when changing this, the struct is exposed directly in the
// 'mender.conf' file.
type HttpsClient struct {
	Certificate string
	Key         string
	SSLEngine   string
}

// Security structure holds the configuration for the client
// Added for MEN-3924 in order to provide a way to specify PKI params
// outside HttpsClient.
// NOTE: Careful when changing this, the struct is exposed directly in the
// 'mender.conf' file.
type Security struct {
	AuthPrivateKey string
	SSLEngine      string
}

func (h *HttpsClient) Validate() {
	if h == nil {
		return
	}
	if h.Certificate != "" || h.Key != "" {
		if h.Certificate == "" {
			log.Error("The 'Key' field is set in the mTLS configuration, but no 'Certificate' is given. Both need to be present in order for mTLS to function")
		}
		if h.Key == "" {
			log.Error("The 'Certificate' field is set in the mTLS configuration, but no 'Key' is given. Both need to be present in order for mTLS to function")
		} else if strings.HasPrefix(h.Key, pkcs11URIPrefix) && len(h.SSLEngine) == 0 {
			log.Errorf("The 'Key' field is set to be loaded from %s, but no 'SSLEngine' is given. Both need to be present in order for loading of the key to function",
				pkcs11URIPrefix)
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
