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
package app

import (
	"os"
	"strings"

	"github.com/mendersoftware/mender/client"
	"github.com/mendersoftware/mender/conf"
	"github.com/mendersoftware/mender/datastore"
	dev "github.com/mendersoftware/mender/device"
	"github.com/mendersoftware/mender/store"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
)

type AuthManager interface {
	// returns true if authorization data is current and valid
	IsAuthorized(conf.MenderConfig) bool
	// returns device's authorization token
	AuthToken() (client.AuthToken, error)
	// updates tenat token
	UpdateAuthTenantToken(tenantToken string) error
	// returns tenant token associated with device's authorization token
	AuthTenantToken() (string, error)
	// returns server URL associated device's authorization token
	AuthServerURL() (string, error)
	// removes authentication token
	RemoveAuthToken() error
	// check if device key is available
	HasKey() bool
	// generate device key (will overwrite an already existing key)
	GenerateKey() error

	client.AuthDataMessenger
}

const (
	noAuthToken = client.EmptyAuthToken
)

type MenderAuthManager struct {
	store       store.Store
	keyStore    *store.Keystore
	idSrc       dev.IdentityDataGetter
	tenantToken client.AuthToken
}

type AuthManagerConfig struct {
	AuthDataStore  store.Store            // authorization data store
	KeyStore       *store.Keystore        // key storage
	IdentitySource dev.IdentityDataGetter // provider of identity data
	TenantToken    []byte                 // tenant token
}

func NewAuthManager(conf AuthManagerConfig) AuthManager {

	if conf.KeyStore == nil || conf.IdentitySource == nil ||
		conf.AuthDataStore == nil {
		return nil
	}

	mgr := &MenderAuthManager{
		store:       conf.AuthDataStore,
		keyStore:    conf.KeyStore,
		idSrc:       conf.IdentitySource,
		tenantToken: client.AuthToken(conf.TenantToken),
	}

	if err := mgr.keyStore.Load(); err != nil && !store.IsNoKeys(err) {
		log.Errorf("Failed to load device keys: %v", err)
		// Otherwise ignore error returned from Load() call. It will
		// just result in an empty keyStore which in turn will cause
		// regeneration of keys.
	}

	return mgr
}

func (m *MenderAuthManager) IsAuthorized(config conf.MenderConfig) bool {
	log.Infof("men-3420 IsAuthorized starting;")
	//serverURL, err := m.AuthServerURL()
	//log.Infof("    IsAuthorized config.ServerURL:%s != serverURL:%s; err=%v.", config.Servers[0].ServerURL, serverURL, err)
	//if err != nil {
	//	return false
	//}
	//if config.Servers[0].ServerURL != serverURL {
	//	return false
	//}
	//
	tenantToken, err := m.AuthTenantToken()
	if err != nil {
		return false
	}
	serverUrl, err := m.AuthServerURL()
	if err != nil {
		return false
	}

	if config.TenantToken != tenantToken || config.ServerURL != serverUrl {
		log.Infof("men-3420    IsAuthorized config.TenantToken:%s != tenantToken:%s; err=%v.", config.TenantToken, tenantToken, err)
		// m.UpdateAuthTenantToken(config.TenantToken) // do we need this?
		m.RemoveAuthToken()
		a, _ := m.AuthToken()
		t, _ := m.AuthTenantToken()
		log.Infof("men-3420    IsAuthorized currently: removed AuthToken, it is: '%s' reset tenant token: '%s', returning false;", a, t)
		return false
	}

	adata, err := m.AuthToken()
	if err != nil {
		return false
	}
	log.Infof("men-3420    IsAuthorized adata:%s", adata)
	if adata == noAuthToken {
		return false
	}

	// TODO check if JWT is valid?

	return true
}

func (m *MenderAuthManager) MakeAuthRequest() (*client.AuthRequest, error) {

	var err error
	authd := client.AuthReqData{}

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

	tentok := strings.TrimSpace(string(m.tenantToken))

	log.Debugf("Tenant token: %s", tentok)

	// fill tenant token
	authd.TenantToken = string(tentok)

	log.Debugf("Authorization data: %v", authd)

	reqdata, err := authd.ToBytes()
	if err != nil {
		return nil, errors.Wrapf(err, "failed to convert auth request data")
	}

	// generate signature
	sig, err := m.keyStore.Sign(reqdata)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to sign auth request")
	}

	return &client.AuthRequest{
		Data:      reqdata,
		Token:     client.AuthToken(tentok),
		Signature: sig,
	}, nil
}

func (m *MenderAuthManager) RecvAuthResponse(data []byte, tenantToken string, serverURl string) error {
	if len(data) == 0 {
		return errors.New("empty auth response data")
	}

	if err := m.store.WriteMap(map[string][]byte{
		datastore.AuthTokenName:       data,
		datastore.AuthTenantTokenName: []byte(tenantToken),
		datastore.AuthServerURLName:   []byte(serverURl),
	}); err != nil {
		return errors.Wrapf(err, "failed to save auth token, tenant token, server url")
	}
	log.Debugf("RecvAuthResponse wrote %s,%s,%s.",
		datastore.AuthTokenName,
		datastore.AuthTenantTokenName,
		datastore.AuthServerURLName)

	//if err := m.store.WriteAll(datastore.AuthTokenName, data); err != nil {
	//	return errors.Wrapf(err, "failed to save auth token")
	//}
	//log.Infof("men-3420 writing k:%s v:%s", datastore.AuthTenantTokenName, tenantToken)
	//if err := m.store.WriteAll(datastore.AuthTenantTokenName, []byte(tenantToken)); err != nil {
	//	return errors.Wrapf(err, "failed to save tenant token")
	//}
	//if err := m.store.WriteAll(datastore.AuthServerURLName, []byte(serverURl)); err != nil {
	//	return errors.Wrapf(err, "failed to save serverURL token")
	//}
	return nil
}

func (m *MenderAuthManager) UpdateAuthTenantToken(tenantToken string) error {
	return m.store.WriteAll(datastore.AuthTenantTokenName, []byte(tenantToken))
}

func (m *MenderAuthManager) AuthTenantToken() (string, error) {
	data, err := m.store.ReadAll(datastore.AuthTenantTokenName)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", errors.Wrapf(err, "failed to read tenant token data")
	}

	return string(data), nil
}

func (m *MenderAuthManager) AuthServerURL() (string, error) {
	data, err := m.store.ReadAll(datastore.AuthServerURLName)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", errors.Wrapf(err, "failed to read server URL data")
	}

	return string(data), nil
}

func (m *MenderAuthManager) AuthToken() (client.AuthToken, error) {
	data, err := m.store.ReadAll(datastore.AuthTokenName)
	if err != nil {
		if os.IsNotExist(err) {
			return noAuthToken, nil
		}
		return noAuthToken, errors.Wrapf(err, "failed to auth token data")
	}

	return client.AuthToken(data), nil
}

func (m *MenderAuthManager) RemoveAuthToken() error {
	// remove token only if we have one
	if aToken, err := m.AuthToken(); err == nil && aToken != noAuthToken {
		return m.store.Remove(datastore.AuthTokenName)
	}
	return nil
}

func (m *MenderAuthManager) HasKey() bool {
	return m.keyStore.Private() != nil
}

func (m *MenderAuthManager) GenerateKey() error {
	if err := m.keyStore.Generate(); err != nil {
		log.Errorf("Failed to generate device key: %v", err)
		return errors.Wrapf(err, "failed to generate device key")
	}

	if err := m.keyStore.Save(); err != nil {
		log.Errorf("Failed to save device key: %s", err)
		return NewFatalError(err)
	}
	return nil
}
