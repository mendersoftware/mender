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
package test

import (
	"bytes"
	"crypto/tls"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"

	log "github.com/sirupsen/logrus"
)

type AuthTestServer struct {
	Server *httptest.Server
	Auth   AuthType
}

type AuthType struct {
	Authorize bool
	Token     []byte

	// If set, is used instead of Authorize and Token.
	AuthFunc func() (bool, []byte)

	Called bool
	Verify bool
}

// Can be several different types, see switch statement inside
// NewAuthTestServer().
type Options interface{}

type CertAndKey struct {
	Certificate []byte
	PrivateKey  []byte
}

func NewAuthTestServer(options ...Options) *AuthTestServer {
	cts := &AuthTestServer{}
	var tlsConfig *tls.Config
	var mux *http.ServeMux
	for _, opt := range options {
		// Accept several types of arguments that can customize the test server.
		switch o := opt.(type) {
		case *tls.Config:
			if tlsConfig != nil {
				panic("Conflicting TLS options in NewAuthTestServer()")
			}
			tlsConfig = o
		case *CertAndKey:
			if tlsConfig != nil {
				panic("Conflicting TLS options in NewAuthTestServer()")
			}
			cert, _ := tls.X509KeyPair(o.Certificate, o.PrivateKey)
			tlsConfig = &tls.Config{
				Certificates: []tls.Certificate{cert},
			}
		case *http.ServeMux:
			mux = o
		default:
			panic(fmt.Sprintf(
				"Unsupported argument type to NewAuthTestServer(): %T",
				opt))
		}
	}

	if mux == nil {
		mux = http.NewServeMux()
	}
	mux.HandleFunc("/api/devices/v1/authentication/auth_requests", cts.authReq)

	cts.Server = httptest.NewUnstartedServer(mux)
	if tlsConfig != nil {
		cts.Server.TLS = tlsConfig
		cts.Server.StartTLS()
	} else {
		cts.Server.Start()
	}
	return cts
}

func (cts *AuthTestServer) Close() {
	cts.Server.Close()
}

func (cts *AuthTestServer) Reset() {
	cts.Auth = AuthType{}
}

func IsMethod(method string, w http.ResponseWriter, r *http.Request) bool {
	if r.Method != method {
		log.Errorf("method verification failed, expected %v got %v",
			method, r.Method)
		w.WriteHeader(http.StatusMethodNotAllowed)
		return false
	}
	return true
}

func IsContentType(ct string, w http.ResponseWriter, r *http.Request) bool {
	rct := r.Header.Get("Content-Type")
	if ct != rct {
		log.Errorf("content-type verification failed, expected %v got %v",
			ct, rct)
		w.WriteHeader(http.StatusUnsupportedMediaType)
		return false
	}
	return true
}

// VerifyAuth checks that client is authorized and returns false if not.
// AuthTestServer.Auth.Verify must be true for verification to take place.
// Client token must match AuthTestServer.Auth.Token.
func (cts *AuthTestServer) VerifyAuth(w http.ResponseWriter, r *http.Request) bool {
	if cts.Auth.Verify {
		hv := r.Header.Get("Authorization")
		if hv == "" {
			log.Errorf("no authorization header")
			w.WriteHeader(http.StatusUnauthorized)
			return false
		}
		if !strings.HasPrefix(hv, "Bearer ") {
			log.Errorf("bad authorization value: %v", hv)
			w.WriteHeader(http.StatusUnauthorized)
			return false
		}

		s := strings.SplitN(hv, " ", 2)
		tok := s[1]

		if !bytes.Equal(cts.Auth.Token, []byte(tok)) {
			log.Errorf("bad token, got %s expected %s", hv, cts.Auth.Token)
			w.WriteHeader(http.StatusUnauthorized)
			return false
		}
	}
	return true
}

func (cts *AuthTestServer) authReq(w http.ResponseWriter, r *http.Request) {
	log.Infof("got auth request %v", r)
	cts.Auth.Called = true

	if !IsMethod(http.MethodPost, w, r) {
		return
	}

	if !IsContentType("application/json", w, r) {
		return
	}

	var authorize bool
	var token []byte
	if cts.Auth.AuthFunc != nil {
		authorize, token = cts.Auth.AuthFunc()
	} else {
		authorize = cts.Auth.Authorize
		token = cts.Auth.Token
	}

	if authorize {
		w.WriteHeader(http.StatusOK)
		w.Header().Set("Content-Type", "text/plain")
		w.Write(token)
	} else {
		w.WriteHeader(http.StatusUnauthorized)
	}
}
