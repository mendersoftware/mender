// Copyright 2022 Northern.tech AS
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

//go:build linux
// +build linux

package artifact

import (
	"encoding/base64"
	"strings"

	"github.com/mendersoftware/openssl"
	"github.com/pkg/errors"
)

const (
	pkcs11URIPrefix = "pkcs11:"
	pkcsEngineId    = "pkcs11"
)

type PKCS11Signer struct {
	Key openssl.PrivateKey
}

func NewPKCS11Signer(pkcsKey string) (*PKCS11Signer, error) {
	if len(pkcsKey) == 0 {
		return nil, errors.New("PKCS#11 signer: missing key")
	}

	key, err := loadPrivateKey(pkcsKey, pkcsEngineId)
	if err != nil {
		return nil, errors.Wrap(err, "PKCS#11: failed to load private key")
	}

	return &PKCS11Signer{
		Key: key,
	}, nil
}

func (s *PKCS11Signer) Sign(message []byte) ([]byte, error) {
	sig, err := s.Key.SignPKCS1v15(openssl.SHA256_Method, message[:])
	if err != nil {
		return nil, errors.Wrap(err, "PKCS#11 signer: error signing image")
	}

	enc := make([]byte, base64.StdEncoding.EncodedLen(len(sig)))
	base64.StdEncoding.Encode(enc, sig)
	return enc, nil
}

func (s *PKCS11Signer) Verify(message, sig []byte) error {
	dec := make([]byte, base64.StdEncoding.DecodedLen(len(sig)))
	decLen, err := base64.StdEncoding.Decode(dec, sig)
	if err != nil {
		return errors.Wrap(err, "signer: error decoding signature")
	}
	err = s.Key.VerifyPKCS1v15(openssl.SHA256_Method, message[:], dec[:decLen])
	return errors.Wrap(err, "failed to verify PKCS#11 signature")
}

var engineLoadPrivateKeyFunc = openssl.EngineLoadPrivateKey

func loadPrivateKey(keyFile string, engineId string) (key openssl.PrivateKey, err error) {
	if strings.HasPrefix(keyFile, pkcs11URIPrefix) {
		engine, err := openssl.EngineById(engineId)
		if err != nil {
			return nil, err
		}

		key, err = engineLoadPrivateKeyFunc(engine, keyFile)
		if err != nil {
			return nil, err
		}
	} else {
		return nil, errors.New("PKCS#11 URI prefix not found")
	}

	return key, nil
}
