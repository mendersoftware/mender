// Copyright 2021 Northern.tech AS
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
package api

import (
	"fmt"
	"io"
	"net/http"
	"runtime"
	"strings"
	"time"

	common "github.com/mendersoftware/mender/common/api"
	"github.com/mendersoftware/mender/common/dbus"
	"github.com/mendersoftware/mender/common/tls"

	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
)

const (
	AuthManagerDBusPath                      = "/io/mender/AuthenticationManager"
	AuthManagerDBusObjectName                = "io.mender.AuthenticationManager"
	AuthManagerDBusInterfaceName             = "io.mender.Authentication1"
	AuthManagerDBusSignalJwtTokenStateChange = "JwtTokenStateChange"
	AuthManagerDBusFetchJwtToken             = "FetchJwtToken"
	AuthManagerDBusGetJwtToken               = "GetJwtToken"

	signalChannelBufferLength = 10
)

// Mender API Client wrapper. A standard http.Client is compatible with this
// interface and can be used without further configuration where ApiRequester is
// expected. Instead of instantiating the client by yourself, one can also use a
// wrapper call NewApiClient() that sets up TLS handling according to passed
// configuration.
type ApiRequester interface {
	Do(req *http.Request) (*http.Response, error)
}

func BuildApiURL(path string) string {
	return common.ApiPrefix + strings.TrimLeft(path, "/")
}

type RequestProcessingFunc func(response *http.Response) (interface{}, error)

type AuthToken = string
type ServerURL = string

// wrapper for http.Client with additional methods
type ApiClient struct {
	http  *http.Client
	https *http.Client

	DBusAPI         dbus.DBusAPI
	jwtStateChanged *dbus.SignalChannel
	authTimeout     time.Duration
	authTokenGetter AuthTokenGetter
	auth            AuthToken
	serverURL       ServerURL
}

type AuthTokenGetter interface {
	GetAuthToken() (AuthToken, ServerURL, error)
}

func NewApiClient(authTimeout time.Duration, conf tls.Config) (*ApiClient, error) {
	httpCli, err := tls.NewHttpOrHttpsClient(tls.Config{})
	if err != nil {
		return nil, err
	}

	httpsCli, err := tls.NewHttpOrHttpsClient(conf)

	aCli := &ApiClient{
		http:        httpCli,
		https:       httpsCli,
		DBusAPI:     dbus.GetDBusAPI(),
		authTimeout: authTimeout,
	}
	// For tests: The call is replacable but use ourselves by default.
	aCli.authTokenGetter = aCli

	return aCli, err
}

func (a *ApiClient) GetAuthToken() (AuthToken, ServerURL, error) {
	bus, err := a.DBusAPI.BusGet(dbus.GBusTypeSystem)
	if err != nil {
		return "", "", errors.Wrap(err, "Could not get auth token")
	}

	params, err := a.DBusAPI.Call0(
		bus,
		AuthManagerDBusObjectName,
		AuthManagerDBusPath,
		AuthManagerDBusInterfaceName,
		AuthManagerDBusGetJwtToken)
	if err != nil {
		return "", "", errors.Wrap(err, "Could not get auth token")
	}

	return a.parseJwtFromDbus(params)
}

func (a *ApiClient) RequestNewAuthToken() (AuthToken, ServerURL, error) {
	if a.jwtStateChanged == nil {
		err := a.setupJwtStateChangedChannel()
		if err != nil {
			return "", "", err
		}
	}

	// Check if there already is a new one, and if so, the latest one.
	var params []interface{}
	for {
		select {
		case params = <-*a.jwtStateChanged:
		default:
			goto exitloop
		}
	}
exitloop:
	// A new token is already available
	if params != nil {
		log.Debug("Signal about new token already received. Not requesting a new one.")
		return a.parseJwtFromDbus(params)
	}

	// No token available, need to request one.

	bus, err := a.DBusAPI.BusGet(dbus.GBusTypeSystem)
	if err != nil {
		return "", "", errors.Wrap(err, "Could not request new auth token")
	}

	log.Debugf("Calling %s and waiting for new token to arrive",
		AuthManagerDBusFetchJwtToken)

	params, err = a.DBusAPI.Call0(
		bus,
		AuthManagerDBusObjectName,
		AuthManagerDBusPath,
		AuthManagerDBusInterfaceName,
		AuthManagerDBusFetchJwtToken)
	if err != nil {
		return "", "", errors.Wrap(err, "Could not request new auth token")
	}
	if len(params) < 1 {
		return "", "", errors.Errorf("Unexpected return from %s, not enough elements",
			AuthManagerDBusFetchJwtToken)
	}
	if value, ok := params[0].(bool); !ok || !value {
		return "", "", errors.Errorf("Did not receive true boolean return from %s: %v",
			AuthManagerDBusFetchJwtToken, value)
	}

	// Wait for a new token to arrive.
	select {
	case params = <-*a.jwtStateChanged:
		return a.parseJwtFromDbus(params)
	case <-time.After(a.authTimeout):
		return "", "", errors.Errorf("Request for new auth token timed out after %d seconds",
			int(a.authTimeout.Seconds()))
	}
}

func (a *ApiClient) setupJwtStateChangedChannel() error {
	DBusAPI := a.DBusAPI
	bus, err := DBusAPI.BusGet(dbus.GBusTypeSystem)
	if err != nil {
		return err
	}

	ch := make(dbus.SignalChannel, signalChannelBufferLength)
	a.jwtStateChanged = &ch
	DBusAPI.RegisterSignalChannel(
		bus,
		AuthManagerDBusObjectName,
		AuthManagerDBusPath,
		AuthManagerDBusInterfaceName,
		AuthManagerDBusSignalJwtTokenStateChange,
		*a.jwtStateChanged)

	runtime.SetFinalizer(a.jwtStateChanged, func(c *dbus.SignalChannel) {
		DBusAPI.UnregisterSignalChannel(
			bus,
			AuthManagerDBusSignalJwtTokenStateChange,
			*c)
	})

	return nil
}

func (a *ApiClient) parseJwtFromDbus(params []interface{}) (AuthToken, ServerURL, error) {
	if len(params) < 2 {
		return "", "", errors.New("Unexpected D-Bus JWT information: Contained less than two elements")
	}
	jwt, jwtOk := params[0].(string)
	url, urlOk := params[1].(string)
	if !jwtOk || !urlOk {
		return "", "", errors.Errorf("Unexpected D-Bus JWT information: Expected two strings, but types were %T and %T",
			params[0], params[1])
	}

	if jwt == "" || url == "" {
		log.Debug("Received empty auth token and/or server from D-Bus")
	} else {
		log.Debugf("Received auth token and server %s from D-Bus", url)
	}

	return jwt, url, nil
}

func errWrapUncond(err error, msg string) error {
	// errors.Wrap() returns nil if err is nil, but we want to return error
	// regardless.
	if err != nil {
		return errors.Wrap(err, msg)
	} else {
		return errors.New(msg)
	}
}

// Do is a wrapper for http.Do function for ApiClient. This function in addition
// to calling http.Do handles Unauthorized secnarios by requesting a new server
// and JWT token over DBus.
func (a *ApiClient) Do(req *http.Request) (*http.Response, error) {
	var r *http.Response
	var err error
	requestNewToken := false
	requestedNewToken := false

	for {
		if a.auth == "" || a.serverURL == "" {
			a.auth, a.serverURL, err = a.authTokenGetter.GetAuthToken()
			if err != nil {
				return nil, err
			}
			if a.auth == "" || a.serverURL == "" {
				requestNewToken = true
			}
		}

		if requestNewToken {
			if requestedNewToken {
				// To avoid loops in case of errors, request new
				// token at most once per request.
				return nil, errWrapUncond(err, "Device unauthorized and already requested new token. Giving up")
			}
			log.Info("Attempting reauthorization")
			a.auth, a.serverURL, err = a.RequestNewAuthToken()
			if err != nil {
				return nil, err
			}
			if a.auth == "" || a.serverURL == "" {
				return nil, errors.Errorf("Not authorized, cannot call API endpoint: %s",
					req.URL.String())
			}
			requestedNewToken = true
		}

		r, err = a.doRequest(req)
		if err == nil && r.StatusCode == http.StatusUnauthorized {
			log.Info("Device unauthorized")
			a.auth = ""
			a.serverURL = ""
			requestNewToken = true
			continue
		} else {
			return r, err
		}
	}
}

func (a *ApiClient) doRequest(req *http.Request) (*http.Response, error) {
	var err error

	log.Debugf("Connecting to server URL %s", a.serverURL)

	var body io.ReadCloser
	if req.GetBody != nil {
		body, err = req.GetBody()
		if err != nil {
			return nil, errors.Wrap(err, "Unable to reconstruct HTTP request body")
		}
	} else {
		body = nil
	}

	// create a new request object to avoid issues when consuming the
	// request body multiple times when failing over a different server. It
	// is not safe to reuse the same request multiple times when
	// request.Body is not nil
	//
	// see: https://github.com/golang/go/issues/19653
	// Error message: http: ContentLength=52 with Body length 0
	urlStr := strings.TrimRight(a.serverURL, "/") + "/" + strings.TrimLeft(req.URL.Path, "/")
	log.Debugf("Connecting to final URL: %s", urlStr)
	if len(req.URL.RawQuery) > 0 {
		// Include query parameters.
		urlStr += "?" + req.URL.RawQuery
	}
	newReq, err := http.NewRequest(req.Method, urlStr, body)
	if err != nil {
		return nil, errors.Wrap(err, "Unable to construct new request")
	}
	newReq.Header = req.Header
	newReq.GetBody = req.GetBody
	newReq.Host = ""

	newReq.Header.Set("Authorization", fmt.Sprintf("Bearer %s", a.auth))

	var cli *http.Client
	if newReq.URL.Scheme == "http" {
		cli = a.http
	} else if newReq.URL.Scheme == "https" {
		cli = a.https
	}

	return cli.Do(newReq)
}

// DownloadApiClient differs from ApiClient in that it doesn't query DBus for
// server URL and authorization token. This is important to not leak
// authorization credentials to download servers. It does, however use the
// custom http.Clients.
type DownloadApiClient struct {
	http  *http.Client
	https *http.Client
}

func NewDownloadApiClient(conf tls.Config) (*DownloadApiClient, error) {
	httpCli, err := tls.NewHttpOrHttpsClient(tls.Config{})
	if err != nil {
		return nil, err
	}

	httpsCli, err := tls.NewHttpOrHttpsClient(conf)

	aCli := &DownloadApiClient{
		http:  httpCli,
		https: httpsCli,
	}

	return aCli, err
}

func (a *DownloadApiClient) Do(req *http.Request) (*http.Response, error) {
	if req.URL.Scheme == "http" {
		return a.http.Do(req)
	} else if req.URL.Scheme == "https" {
		return a.https.Do(req)
	} else {
		return nil, errors.Errorf("Unsupported URL scheme: %s", req.URL.Scheme)
	}
}

// Normally one minute, but used in tests to lower the interval to avoid
// waiting.
var ExponentialBackoffSmallestUnit time.Duration = time.Minute

var MaxRetriesExceededError = errors.New("Tried maximum amount of times")

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
