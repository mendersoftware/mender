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
package store

import (
	"bytes"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"io"
	"io/ioutil"
	"os"

	"github.com/mendersoftware/log"
	"github.com/pkg/errors"
)

const (
	RsaKeyLength = 3072
)

var (
	errNoKeys = errors.New("no keys")
)

type Keystore struct {
	store            Store
	private          *rsa.PrivateKey
	encryptionSecret string
	keyName          string
}

func (k *Keystore) GetStore() Store {
	return k.store
}

func (k *Keystore) GetPrivateKey() *rsa.PrivateKey {
	return k.private
}

func (k *Keystore) GetKeyName() string {
	return k.keyName
}

func NewKeystore(store Store, name string, encryptionSecret string) *Keystore {
	if store == nil {
		return nil
	}

	return &Keystore{
		store:            store,
		keyName:          name,
		encryptionSecret: encryptionSecret,
	}
}

func (k *Keystore) Load() error {
	inf, err := k.store.OpenRead(k.keyName)
	if err != nil {
		if os.IsNotExist(err) {
			log.Debugf("private key does not exist")
			return errNoKeys
		}
		return err
	}
	defer inf.Close()

	if k.encryptionSecret == "" {
		k.private, err = loadFromPem(inf)
	} else {
		k.private, err = loadFromPemWithSecret(inf, []byte(k.encryptionSecret))
	}
	if err != nil {
		log.Errorf("failed to load key: %s", err)
		return err
	}

	return nil
}

func (k *Keystore) Save() error {
	if k.private == nil {
		return errNoKeys
	}

	outf, err := k.store.OpenWrite(k.keyName)
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

func (k *Keystore) Public() crypto.PublicKey {
	if k.private != nil {
		return k.private.Public()
	}
	return nil
}

func (k *Keystore) PublicPEM() (string, error) {
	data, err := x509.MarshalPKIXPublicKey(k.Public())
	if err != nil {
		return "", errors.Wrapf(err, "failed to marshal public key")
	}

	buf := &bytes.Buffer{}
	err = pem.Encode(buf, &pem.Block{
		Type:  "PUBLIC KEY", // PKCS1
		Bytes: data,
	})
	if err != nil {
		return "", errors.Wrapf(err, "failed to encode public key to PEM")
	}

	return buf.String(), nil
}

func (k *Keystore) Sign(data []byte) ([]byte, error) {
	hash := crypto.SHA256
	h := hash.New()
	h.Write(data)
	sum := h.Sum(nil)

	return rsa.SignPKCS1v15(rand.Reader, k.private, hash, sum)
}

func IsNoKeys(e error) bool {
	return e == errNoKeys
}

func loadFromPemWithSecret(in io.Reader, secret []byte) (*rsa.PrivateKey, error) {
	data, err := ioutil.ReadAll(in)
	if err != nil {
		return nil, err
	}

	block, _ := pem.Decode(data)
	dblock, err := x509.DecryptPEMBlock(block, secret)
	if err != nil {
		return nil, err
	}

	key, err := x509.ParsePKCS1PrivateKey(dblock)
	if err != nil {
		return nil, err
	}

	return key, nil
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
