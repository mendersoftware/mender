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
	"bytes"
	"crypto/x509"
	"encoding/base64"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"time"

	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
)

var AuthErrorUnauthorized = errors.New("authentication request rejected")

type AuthRequester interface {
	Request(api ApiRequester, server string, dataSrc AuthDataMessenger) ([]byte, error)
}

// Auth client wrapper. Instantiate by yourself or use `NewAuthClient()` helper
type AuthClient struct {
}

func NewAuth() *AuthClient {
	ac := AuthClient{}
	return &ac
}

func (u *AuthClient) Request(api ApiRequester, server string, dataSrc AuthDataMessenger) ([]byte, error) {

	req, err := makeAuthRequest(server, dataSrc)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to build authorization request")
	}

	log.Debugf("Making an authorization request (%s) to server %s", req.RequestURI, server)
	rsp, err := api.Do(req)
	if err != nil {
		// checking the detailed reason of the failure
		if urlErr, ok := err.(*url.Error); ok {
			log.Errorf("Failure occurred while executing authorization request: Method: %s, URL: %s", urlErr.Op, urlErr.URL)

			switch certErr := urlErr.Err.(type) {
			case x509.UnknownAuthorityError:
				log.Error("Certificate is signed by unknown authority.")
				log.Error("If you are using a self-signed certificate, make sure it is " +
					"available locally to the Mender client in /etc/mender/server.crt and " +
					"is configured properly in /etc/mender/mender.conf.")
				log.Error("See https://docs.mender.io/troubleshooting/mender-client#" +
					"certificate-signed-by-unknown-authority for more information.")

				return nil, errors.Wrapf(err, "certificate signed by unknown authority")

			case x509.CertificateInvalidError:
				switch certErr.Reason {
				case x509.Expired:
					log.Error("Certificate has expired or is not yet valid.")
					log.Errorf("Current clock is %s", time.Now())
					log.Error("Verify that the clock on the device is correct " +
						"and/or certificate expiration date is valid.")
					log.Error("See https://docs.mender.io/troubleshooting/mender-client#" +
						"certificate-expired-or-not-yet-valid for more information.")

					return nil, errors.Wrapf(err, "certificate has expired")
				default:
					log.Errorf("Server certificate is invalid, reason: %#v", certErr.Reason)
				}
				return nil, errors.Wrapf(err, "certificate exists, but is invalid")
			default:
				return nil, errors.Wrap(certErr, "Unknown url.Error type")
			}
		}
		return nil, errors.Wrapf(err,
			"generic error occurred while executing authorization request")
	}
	defer rsp.Body.Close()

	log.Debugf("Got response: %v", rsp)

	switch rsp.StatusCode {
	case http.StatusUnauthorized:
		return nil, NewAPIError(AuthErrorUnauthorized, rsp)
	case http.StatusOK:
		log.Debugf("Receive response data")
		data, err := ioutil.ReadAll(rsp.Body)
		if err != nil {
			return nil, NewAPIError(errors.Wrapf(err, "failed to receive authorization response data"), rsp)
		}

		log.Debugf("Received response data:  %v", data)
		return data, nil
	default:
		return nil, NewAPIError(errors.Errorf("unexpected authorization status %v", rsp.StatusCode), rsp)
	}
}

func makeAuthRequest(server string, dataSrc AuthDataMessenger) (*http.Request, error) {
	url := buildApiURL(server, "/authentication/auth_requests")

	req, err := dataSrc.MakeAuthRequest()
	if err != nil {
		return nil, errors.Wrapf(err, "failed to obtain authorization message data")
	}

	dataio := bytes.NewBuffer(req.Data)
	hreq, err := http.NewRequest(http.MethodPost, url, dataio)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to create authorization HTTP request")
	}

	hreq.Header.Add("Content-Type", "application/json")
	hreq.Header.Add("Authorization", fmt.Sprintf("Bearer %s", req.Token))
	hreq.Header.Add("X-MEN-Signature", base64.StdEncoding.EncodeToString(req.Signature))
	return hreq, nil
}
