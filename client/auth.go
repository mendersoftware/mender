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
	"encoding/json"

	"github.com/pkg/errors"
)

const (
	EmptyAuthToken = AuthToken("")
)

type AuthToken string

// Structure representing authorization request data. The caller must fill each
// field.
type AuthReqData struct {
	// identity data
	IdData string `json:"id_data"`
	// tenant token
	TenantToken string `json:"tenant_token"`
	// client's public key
	Pubkey string `json:"pubkey"`
}

// Produce a raw byte sequence with authorization data encoded in a format
// expected by the backend
func (ard *AuthReqData) ToBytes() ([]byte, error) {
	databuf := &bytes.Buffer{}
	enc := json.NewEncoder(databuf)

	err := enc.Encode(&ard)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to encode auth request")
	}

	return databuf.Bytes(), nil
}

// A wrapper for authorization request
type AuthRequest struct {
	// raw request message data
	Data []byte
	// tenant's authorization token
	Token AuthToken
	// request signature
	Signature []byte
}

// Interface capturing functionality of generating authorization messages
type AuthDataMessenger interface {
	// Build authorization request data, returns auth request or an error
	MakeAuthRequest() (*AuthRequest, error)
}
