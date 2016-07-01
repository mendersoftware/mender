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
	"bytes"
	"encoding/json"
	"os"

	"github.com/mendersoftware/log"
	"github.com/pkg/errors"
)

type AuthToken string

type AuthReqData struct {
	IdData      string `json:"id_data"`
	TenantToken string `json:"tenant_token"`
	Pubkey      string `json:"pubkey"`
	SeqNumber   uint64 `json:"seq_no"`
}

type AuthRequest struct {
	// request message data
	Data []byte
	// authorization token
	Token AuthToken
	// request signature
	Signature []byte
}

// handler for authorization message data
type AuthDataMessenger interface {
	// build authorization request data, returns auth request or an error
	MakeAuthRequest() (*AuthRequest, error)
	// receive authoiriation data response, returns error if response is invalid
	RecvAuthResponse([]byte) error
}

type AuthManager interface {
	// returns true if authorization data is current and valid
	IsAuthorized() bool
	// returns device's authorization token
	AuthToken() (AuthToken, error)
	// check if device key is available
	HasKey() bool
	// generate device key (will overwrite an already existing key)
	GenerateKey() error

	AuthDataMessenger
}

const (
	authTokenName       = "authtoken"
	authTenantTokenName = "authtentoken"
	authSeqName         = "authseq"

	noAuthToken = AuthToken("")
)

type MenderAuthManager struct {
	store    Store
	keyStore *Keystore
	keyName  string
	idSrc    IdentityDataGetter
	seqNum   SeqnumGetter
}

func NewAuthManager(store Store, keyName string, idSrc IdentityDataGetter) AuthManager {
	ks := NewKeystore(store)
	if ks == nil {
		return nil
	}

	mgr := &MenderAuthManager{
		store:    store,
		keyStore: ks,
		keyName:  keyName,
		idSrc:    idSrc,
		seqNum:   NewFileSeqnum(authSeqName, store),
	}

	if err := ks.Load(keyName); err != nil && !IsNoKeys(err) {
		log.Errorf("failed to load device keys from %v: %v", keyName, err)
		return nil
	}

	return mgr
}

func (m *MenderAuthManager) IsAuthorized() bool {
	adata, err := m.AuthToken()
	if err != nil {
		return false
	}

	if adata == noAuthToken {
		return false
	}

	// TODO check if JWT is valid?

	return true
}

func (m *MenderAuthManager) MakeAuthRequest() (*AuthRequest, error) {

	var err error
	authd := AuthReqData{}

	idata, err := m.idSrc.Get()
	if err != nil {
		return nil, errors.Wrapf(err, "failed to obtain identity data")
	}

	authd.IdData = idata

	// fill device public key
	authd.Pubkey, err = m.keyStore.PublicPEM()
	if err != nil {
		return nil, errors.Wrapf(err, "failed to obtain device public key")
	}

	tentok, err := m.store.ReadAll(authTenantTokenName)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to read tenant token")
	}
	log.Debugf("tenant token: %s", tentok)

	// fill tenant token
	authd.TenantToken = string(tentok)

	// fetch sequence number
	num, err := m.seqNum.Get()
	if err != nil {
		return nil, errors.Wrapf(err, "failed to obtain sequence number")
	}
	authd.SeqNumber = num

	log.Debugf("authorization data: %v", authd)

	databuf := &bytes.Buffer{}
	enc := json.NewEncoder(databuf)

	err = enc.Encode(&authd)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to encode auth request")
	}

	reqdata := databuf.Bytes()

	// generate signature
	sig, err := m.keyStore.Sign(reqdata)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to sign auth request")
	}

	return &AuthRequest{
		Data:      reqdata,
		Token:     AuthToken(tentok),
		Signature: sig,
	}, nil
}

func (m *MenderAuthManager) RecvAuthResponse(data []byte) error {
	if len(data) == 0 {
		return errors.New("empty auth response data")
	}

	if err := m.store.WriteAll(authTokenName, data); err != nil {
		return errors.Wrapf(err, "failed to save auth token")
	}
	return nil
}

func (m *MenderAuthManager) AuthToken() (AuthToken, error) {
	data, err := m.store.ReadAll(authTokenName)
	if err != nil {
		if os.IsNotExist(err) {
			return noAuthToken, nil
		}
		return noAuthToken, errors.Wrapf(err, "failed to read auth token data")
	}

	return AuthToken(data), nil
}

func (m *MenderAuthManager) HasKey() bool {
	return m.keyStore.Private() != nil
}

func (m *MenderAuthManager) GenerateKey() error {
	if err := m.keyStore.Generate(); err != nil {
		log.Errorf("failed to generate device key: %v", err)
		return errors.Wrapf(err, "failed to generate device key")
	}

	if err := m.keyStore.Save(m.keyName); err != nil {
		log.Errorf("failed to save keys to %s: %s", m.keyName, err)
		return NewFatalError(err)
	}
	return nil
}
