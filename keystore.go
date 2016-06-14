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
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"io"
	"io/ioutil"
	"os"

	"github.com/mendersoftware/log"
)

const (
	RsaKeyLength = 2048
)

var (
	errNoKeys = errors.New("no keys")
)

type Keystore struct {
	store   Store
	private *rsa.PrivateKey
}

func NewKeystore(store Store) *Keystore {
	if store == nil {
		return nil
	}

	return &Keystore{
		store: store,
	}
}

func (k *Keystore) Load(privPath string) error {
	inf, err := k.store.OpenRead(privPath)
	if err != nil {
		if os.IsNotExist(err) {
			log.Debugf("private key does not exist")
			return errNoKeys
		} else {
			return err
		}
	}
	defer inf.Close()

	k.private, err = loadFromPem(inf)
	if err != nil {
		log.Errorf("failed to load key: %s", err)
		return err
	}

	return nil
}

func (k *Keystore) Save(privPath string) error {
	if k.private == nil {
		return errNoKeys
	}

	outf, err := k.store.OpenWrite(privPath)
	if err != nil {
		return err
	}

	err = saveToPem(outf, k.private)
	if err != nil {
		// make sure to close the file
		outf.Close()

		log.Errorf("failed to save key: %s", err)
		return err
	}

	outf.Close()

	return outf.Commit()
}

func (k *Keystore) Generate() error {
	key, err := rsa.GenerateKey(rand.Reader, RsaKeyLength)
	if err != nil {
		return err
	}

	k.private = key

	return nil
}

func (k *Keystore) Private() *rsa.PrivateKey {
	return k.private
}

func IsNoKeys(e error) bool {
	return e == errNoKeys
}

func loadFromPem(in io.Reader) (*rsa.PrivateKey, error) {
	data, err := ioutil.ReadAll(in)
	if err != nil {
		return nil, err
	}

	block, _ := pem.Decode(data)
	if block == nil {
		return nil, errors.New("failed to decode block")
	}

	log.Debugf("block type: %s", block.Type)

	key, err := x509.ParsePKCS1PrivateKey(block.Bytes)
	if err != nil {
		return nil, err
	}

	return key, nil
}

func saveToPem(out io.Writer, key *rsa.PrivateKey) error {
	data := x509.MarshalPKCS1PrivateKey(key)

	err := pem.Encode(out, &pem.Block{
		Type:  "RSA PRIVATE KEY", // PKCS1
		Bytes: data,
	})
	return err
}
