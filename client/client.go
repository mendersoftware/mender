// Copyright 2023 Northern.tech AS
//
//	Licensed under the Apache License, Version 2.0 (the "License");
//	you may not use this file except in compliance with the License.
//	You may obtain a copy of the License at
//
//	    http://www.apache.org/licenses/LICENSE-2.0
//
//	Unless required by applicable law or agreed to in writing, software
//	distributed under the License is distributed on an "AS IS" BASIS,
//	WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//	See the License for the specific language governing permissions and
//	limitations under the License.
package client

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path"
	"strings"
	"time"

	"io/ioutil"

	"github.com/gorilla/websocket"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"

	"github.com/mendersoftware/mender/utils"
	"github.com/mendersoftware/openssl"
)

const (
	apiPrefix = "/api/devices/"

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

	ErrClientUnauthorized = errors.New("Client is unauthorized")
)

// Mender API Client wrapper. A standard http.Client is compatible with this
// interface and can be used without further configuration where ApiRequester is
// expected. Instead of instantiating the client by yourself, one can also use a
// wrapper call NewApiClient() that sets up TLS handling according to passed
// configuration.
type ApiRequester interface {
	Do(req *http.Request) (*http.Response, error)
}

// An ApiRequester which internally authorizes automatically. It's possible to
// call ClearAuthorization() in order to force reauthorization.
type AuthorizedApiRequester interface {
	ApiRequester
	ClearAuthorization()
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

	err := a.error.Error()

	if a.reqID != "" {
		err = fmt.Sprintf("(request_id: %s): %s", a.reqID, err)
	}

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

func (a *APIError) Unwrap() error {
	return a.error
}

type ApiClient struct {
	http.Client
}

type ReauthorizingClient struct {
	ApiClient
	// authorization code to use for requests
	auth AuthToken
	// server to use for requests
	serverURL ServerURL
	// anonymous function to initiate reauthorization
	revoke ClientReauthorizeFunc
}

// function type for reauthorization closure (see func reauthorize@mender.go)
type ClientReauthorizeFunc func() (AuthToken, ServerURL, error)

// Reconstruct the given request, taking JWT token and serverURL into account.
func (c *ReauthorizingClient) reconstructRequest(req *http.Request) (*http.Request, error) {
	if c.serverURL == "" {
		return nil, ErrClientUnauthorized
	}

	serverURL, err := url.Parse(string(c.serverURL))
	if err != nil {
		return nil, errors.Wrap(err, "Could not parse ServerURL from auth manager")
	}

	// First prefill newURL with existing URL.
	newURL, _ := url.Parse(req.URL.String())

	// Then selectively fill in the prefix from ServerURL.
	newURL.Scheme = serverURL.Scheme
	newURL.Host = serverURL.Host
	newURL.Path = strings.TrimRight(serverURL.Path, "/") +
		"/" +
		strings.TrimLeft(req.URL.Path, "/")

	log.Debugf("Connecting to server %s", serverURL.String())

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
	newReq, _ := http.NewRequest(req.Method, newURL.String(), body)
	newReq.Header = req.Header
	newReq.GetBody = req.GetBody
	newReq.Header.Set("Authorization", fmt.Sprintf("Bearer %s", c.auth))

	return newReq, nil
}

// Do is a wrapper for http.Do function for ApiRequests. This function in
// addition to calling http.Do handles client-server authorization header /
// reauthorization, as well as attempting failover servers (if given) whenever
// the server "refuse" to serve the request.
func (c *ReauthorizingClient) Do(req *http.Request) (*http.Response, error) {
	fetchedNewToken := false
	for {
		var r *http.Response
		newReq, err := c.reconstructRequest(req)
		if err == nil {
			r, err = c.ApiClient.Do(newReq)
		} else if err != ErrClientUnauthorized {
			return nil, err
		}

		// If we haven't yet fetched a new token, and the call results
		// in connection error or Unauthorized status, try to get
		// one. Otherwise return the result as is.
		if !fetchedNewToken && (err != nil || r.StatusCode == http.StatusUnauthorized) {
			// Try to fetch a new JWT token from one of the servers in the server
			// list.
			log.Info("Device unauthorized; attempting reauthorization")
			jwt, serverURL, e := c.revoke()
			if e == nil {
				// retry API request with new JWT token
				c.auth = jwt
				c.serverURL = serverURL
				log.Info("Reauthorization successful")
			} else {
				log.Warnf("Reauthorization failed with error: %s", e.Error())
				return nil, e
			}
			fetchedNewToken = true
		} else {
			return r, err
		}
	}
}

func (c *ReauthorizingClient) ClearAuthorization() {
	c.auth = ""
	c.serverURL = ""
}

func NewReauthorizingClient(
	conf Config,
	reauth ClientReauthorizeFunc,
) (*ReauthorizingClient, error) {
	client, err := NewApiClient(conf)
	if err != nil {
		return nil, err
	}
	return &ReauthorizingClient{
		ApiClient: *client,
		revoke:    reauth,
	}, nil
}

func NewApiClient(conf Config) (*ApiClient, error) {

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
		return 0, fmt.Errorf(
			"Failed to read the OpenSSL default directory (%s). Err %v",
			certDir,
			err.Error(),
		)
	}
	for _, certFile := range files {
		// Need to re-stat here because ReadDir does not resolve
		// symlinks.
		info, err := os.Stat(path.Join(certDir, certFile.Name()))
		if err != nil {
			log.Debugf("Failed to stat file %s: %s", certFile.Name(), err.Error())
			continue
		} else if !info.Mode().IsRegular() {
			log.Debugf("Not a regular file, skipping: %s", info.Name())
			continue
		}

		certBytes, err := ioutil.ReadFile(path.Join(certDir, certFile.Name()))
		if err != nil {
			log.Debugf(
				"Failed to read the certificate file for the HttpsClient. Err %v",
				err.Error(),
			)
			continue
		}

		certs := openssl.SplitPEM(certBytes)
		if len(certs) == 0 {
			log.Debugf("No PEM certificate found in '%s'", certFile)
			continue
		}
		for _, cert := range certs {
			_, err = openssl.LoadCertificateFromPEM(cert)
			if err != nil {
				log.Debug(err.Error())
			} else {
				sysCertsFound += 1
			}
		}
	}
	return sysCertsFound, nil
}

func loadServerTrust(ctx *openssl.Ctx, conf *Config) (*openssl.Ctx, error) {
	defaultCertDir, err := openssl.GetDefaultCertificateDirectory()
	if err != nil {
		return ctx, errors.Wrap(
			err,
			"Failed to get the default OpenSSL certificate directory. Please verify the"+
				" OpenSSL setup",
		)
	}
	sysCertsFound, err := nrOfSystemCertsFound(defaultCertDir)
	if err != nil {
		log.Warnf("Failed to list the system certificates with error: %s", err.Error())
	}

	// Set the default system certificate path for this OpenSSL context
	err = ctx.SetDefaultVerifyPaths()
	if err != nil {
		return ctx, fmt.Errorf(
			"Failed to set the default OpenSSL directory. OpenSSL error code: %s",
			err.Error(),
		)
	}
	if conf.ServerCert != "" {
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
	} else if sysCertsFound == 0 {
		log.Warn(errMissingCerts)
	}
	return ctx, err
}

func loadPrivateKey(keyFile string, engineId string) (key openssl.PrivateKey, err error) {
	if utils.Ispkcs11_keystring(keyFile) || utils.Istpm2tss_keystring(keyFile) {
		// Set keystring based on if pkcs11 or tpm2tss engine
		keyFile = utils.Parsed_keystring(keyFile)
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
		log.Infof("loaded private key: from '%s'.", engineId)
	} else {
		keyBytes, err := ioutil.ReadFile(keyFile)
		if err != nil {
			return nil, errors.Wrap(err, "Private key file from the HttpsClient configuration"+
				" not found")
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

func establishSSLConnection(
	addr string,
	proxyURL *url.URL,
	ctx *openssl.Ctx,
	flags openssl.DialFlags,
) (*openssl.Conn, error) {
	if proxyURL != nil {
		proxyConn, err := dialProxy("tcp", addr, proxyURL)
		if err != nil {
			return nil, errors.Wrap(err, "Failed to connect to proxy")
		}
		return openssl.DialUpgrade(addr, proxyConn, ctx, flags)
	}
	return openssl.Dial("tcp", addr, ctx, flags)
}

func dialOpenSSL(
	ctx *openssl.Ctx,
	conf *Config,
	_, addr string,
) (net.Conn, error) {
	proxyURL, err := ProxyURLFromHostPortGetter(addr)
	if err != nil {
		return nil, errors.Wrapf(err,
			"Failed to get http-proxy configurations during dial")
	}
	flags := openssl.DialFlags(0)

	if conf.NoVerify {
		flags = openssl.InsecureSkipHostVerification
	}

	conn, err := establishSSLConnection(addr, proxyURL, ctx, flags)
	if err != nil {
		return nil, err
	}

	if conf.NoVerify {
		return conn, nil
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

func newOpenSSLCtx(conf Config) (*openssl.Ctx, error) {
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

	return ctx, nil
}

func newHttpsClient(conf Config) (*http.Client, error) {
	ctx, err := newOpenSSLCtx(conf)
	if err != nil {
		return nil, err
	}

	disableKeepAlive := false
	idleConnTimeoutSeconds := 0
	if conf.Connectivity != nil {
		disableKeepAlive = conf.Connectivity.DisableKeepAlive
		idleConnTimeoutSeconds = conf.Connectivity.IdleConnTimeoutSeconds
	}
	transport := http.Transport{
		DisableKeepAlives: disableKeepAlive,
		IdleConnTimeout:   time.Duration(idleConnTimeoutSeconds) * time.Second,
		DialTLS: func(network string, addr string) (net.Conn, error) {
			return dialOpenSSL(ctx, &conf, network, addr)
		},
	}

	client := newHttpClient()
	client.Transport = &transport
	return client, nil
}

// Client configuration

// HttpsClient holds the configuration for the client side mTLS configuration
// NOTE: Careful when changing this, the struct is exposed directly in the
// 'mender.conf' file.
type HttpsClient struct {
	Certificate string `json:",omitempty"`
	Key         string `json:",omitempty"`
	SSLEngine   string `json:",omitempty"`
}

// Security structure holds the configuration for the client
// Added for MEN-3924 in order to provide a way to specify PKI params
// outside HttpsClient.
// NOTE: Careful when changing this, the struct is exposed directly in the
// 'mender.conf' file.
type Security struct {
	AuthPrivateKey string `json:",omitempty"`
	SSLEngine      string `json:",omitempty"`
}

// Connectivity instructs the client how we want to treat the keep alive connections
// and when a connection is considered idle and therefore closed
// NOTE: Careful when changing this, the struct is exposed directly in the
// 'mender.conf' file.
type Connectivity struct {
	// If set to true, there will be no persistent connections, and every
	// HTTP transaction will try to establish a new connection
	DisableKeepAlive bool `json:",omitempty"`
	// A number of seconds after which a connection is considered idle and closed.
	// The longer this is the longer connections are up after the first call over HTTP
	IdleConnTimeoutSeconds int `json:",omitempty"`
}

func (h *HttpsClient) Validate() {
	if h == nil {
		return
	}
	if h.Certificate != "" || h.Key != "" {
		if h.Certificate == "" {
			log.Error(
				"The 'Key' field is set in the mTLS configuration, but no 'Certificate' is given." +
					" Both need to be present in order for mTLS to function",
			)
		}
		if h.Key == "" {
			log.Error(
				"The 'Certificate' field is set in the mTLS configuration, but no 'Key' is given." +
					" Both need to be present in order for mTLS to function",
			)
		} else if strings.HasPrefix(h.Key, pkcs11URIPrefix) && len(h.SSLEngine) == 0 {
			log.Errorf("The 'Key' field is set to be loaded from %s, but no 'SSLEngine' is given."+
				" Both need to be present in order for loading of the key to function",
				pkcs11URIPrefix)
		}
	}
}

type Config struct {
	ServerCert string
	*HttpsClient
	*Connectivity
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

var MaxRetriesExceededError = errors.New("Tried maximum amount of times")

// Simple algorithm: Start with one minute, and try three times, then double
// interval (maxInterval is maximum) and try again. Repeat until we tried
// three times with maxInterval.
func GetExponentialBackoffTime(tried int,
	maxInterval time.Duration,
	maxAttempts int) (time.Duration, error) {
	const perIntervalAttempts = 3

	interval := 1 * ExponentialBackoffSmallestUnit
	nextInterval := interval

	if maxAttempts > 0 && tried >= maxAttempts {
		return 0, MaxRetriesExceededError
	}

	for c := 0; c <= tried; c += perIntervalAttempts {
		interval = nextInterval
		nextInterval *= 2
		if interval >= maxInterval {
			if maxAttempts <= 0 && tried-c >= perIntervalAttempts {
				// At max interval and already tried three
				// times. Give up.
				return 0, MaxRetriesExceededError
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
	resp, err := ioutil.ReadAll(r)
	if err != nil {
		return fmt.Sprintf("Failed to read the response body %s", err)
	}
	if err = json.Unmarshal(resp, e); err != nil {
		return string(resp)
	}
	return e.Error
}

func newWebsocketDialerTLS(conf Config) (*websocket.Dialer, error) {
	ctx, err := newOpenSSLCtx(conf)
	if err != nil {
		return nil, err
	}

	dialer := websocket.Dialer{
		NetDialTLSContext: func(_ context.Context, network string, addr string) (net.Conn, error) {
			return dialOpenSSL(ctx, &conf, network, addr)
		},
	}

	return &dialer, nil
}

// http.ProxyFromEnvironment returns nil if parameter req.URL is localhost.
// This function variable is public so it can be overriden in tests.
var ProxyURLFromHostPortGetter = func(addr string) (*url.URL, error) {
	u, err := url.Parse("https://" + addr)
	if err != nil {
		return u, err
	}
	return http.ProxyFromEnvironment(&http.Request{URL: u})
}

func getHostPort(u *url.URL) (hostPort string) {
	hostPort = u.Host
	if i := strings.LastIndex(u.Host, ":"); i > strings.LastIndex(u.Host, "]") {
		return u.Host
	} else {
		switch u.Scheme {
		case "https":
			hostPort += ":443"
		default:
			hostPort += ":80"
		}
	}
	return hostPort
}

func dialProxy(network string, addr string, proxyURL *url.URL) (net.Conn, error) {
	var (
		resp *http.Response
		err  error
	)
	hostPort := getHostPort(proxyURL)
	conn, err := net.Dial(network, hostPort)
	if err != nil {
		return nil, err
	}

	connectHeader := make(http.Header)
	if user := proxyURL.User; user != nil {
		proxyUser := user.Username()
		if proxyPassword, passwordSet := user.Password(); passwordSet {
			credential := base64.StdEncoding.EncodeToString([]byte(proxyUser + ":" + proxyPassword))
			connectHeader.Set("Proxy-Authorization", "Basic "+credential)
		}
	}

	connectReq := &http.Request{
		Method: http.MethodConnect,
		URL:    &url.URL{Opaque: addr},
		Host:   addr,
		Header: connectHeader,
	}
	connectCtx, cancel := context.WithTimeout(context.Background(), 1*time.Minute)
	defer cancel()

	didReadResponse := make(chan struct{})

	go func() {
		defer close(didReadResponse)
		if err := connectReq.Write(conn); err != nil {
			return
		}
		br := bufio.NewReader(conn)
		resp, err = http.ReadResponse(br, connectReq) //nolint:bodyclose
	}()
	select {
	case <-connectCtx.Done():
		conn.Close()
		<-didReadResponse
		return nil, connectCtx.Err()
	case <-didReadResponse:
	}

	if err != nil {
		conn.Close()
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		conn.Close()
		return nil, errors.Errorf("Response line received from proxy: %s", resp.Status)
	}
	return conn, nil
}

func NewWebsocketDialer(conf Config) (*websocket.Dialer, error) {
	var dialer *websocket.Dialer
	var err error
	if conf == (Config{}) {
		dialer = websocket.DefaultDialer
	} else {
		dialer, err = newWebsocketDialerTLS(conf)
	}
	if err != nil {
		return nil, err
	}

	return dialer, nil
}
