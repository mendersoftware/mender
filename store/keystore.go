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
	"io"
	"io/ioutil"
	"os"
	"strings"

	"github.com/mendersoftware/openssl"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
)

const (
	RsaKeyLength = 3072
)

var (
	errNoKeys    = errors.New("no keys")
	errNoEngines = errors.New("no engines loaded")
	errStaticKey = errors.New("cannot replace static key")
)

type Keystore struct {
	store         Store
	private       openssl.PrivateKey
	keyName       string
	keyPassphrase string
	sslEngine     string
	staticKey     bool
}

func (k *Keystore) GetStore() Store {
	return k.store
}

func (k *Keystore) GetKeyName() string {
	return k.keyName
}

func NewKeystore(store Store, name string, sslEngine string, static bool, passphrase string) *Keystore {
	if store == nil {
		return nil
	}

	return &Keystore{
		store:         store,
		keyName:       name,
		sslEngine:     sslEngine,
		staticKey:     static,
		keyPassphrase: passphrase,
	}
}

func (k *Keystore) Load() error {
	if strings.HasPrefix(k.keyName, "pkcs11:") {
		engine, err := openssl.EngineById(k.sslEngine)
		if err != nil {
			log.Errorf("Failed to Load '%s' engine. Err %s",
				k.sslEngine, err.Error())
			return errNoEngines
		}

		k.private, err = openssl.EngineLoadPrivateKey(engine, k.keyName)
		if err != nil {
			log.Errorf("Failed to Load private key from engine '%s'. Err %s",
				k.sslEngine, err.Error())
			return errNoKeys
		}
	}
	inf, err := k.store.OpenRead(k.keyName)
	if err != nil {
		if os.IsNotExist(err) {
			log.Debugf("Private key does not exist")
			return errNoKeys
		}
		return err
	}
	defer inf.Close()

	passphrase, err := loadPassphrase(k.keyPassphrase)
	if err != nil {
		log.Errorf("Failed to load passphrase-file parameter: %s", err)
		return err
	}

	k.private, err = loadFromPem(inf, passphrase)
	if err != nil {
		log.Errorf("Failed to load key: %s", err)
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
	defer outf.Close()

	err = saveToPem(outf, k.private)
	if err != nil {
		// make sure to close the file
		return err
	}

	return outf.Commit()
}

func (k *Keystore) Generate() error {
	if k.staticKey {
		// Don't re-generate key if it's static.
		return errStaticKey
	}

	key, err := openssl.GenerateRSAKey(RsaKeyLength)
	if err != nil {
		return err
	}

	k.private = key

	return nil
}

func (k *Keystore) Private() openssl.PrivateKey {
	return k.private
}

func (k *Keystore) Public() openssl.PublicKey {
	if k.private != nil {
		return k.private
	}
	return nil
}

func (k *Keystore) PublicPEM() (string, error) {
	if k.private == nil {
		return "", errors.Errorf("private key is empty; ks '%v'", k)
	}

	data, err := k.private.MarshalPKIXPublicKeyPEM()
	if err != nil {
		return "", errors.Wrapf(err, "failed to marshal public key")
	}

	return string(data), err
}

func (k *Keystore) Sign(data []byte) ([]byte, error) {
	method := openssl.SHA256_Method
	if k.private.KeyType() == openssl.KeyTypeED25519 {
		method = nil
	}
	return k.private.SignPKCS1v15(method, data)
}

func IsNoKeys(e error) bool {
	return e == errNoKeys
}

func IsStaticKey(err error) bool {
	return err == errStaticKey
}

func loadFromPem(in io.Reader, keyPassphrase string) (openssl.PrivateKey, error) {
	data, err := ioutil.ReadAll(in)
	var key openssl.PrivateKey
	if err != nil {
		return nil, err
	}
	if keyPassphrase != "" {
		key, err = openssl.LoadPrivateKeyFromPEMWithPassword(data, keyPassphrase)
	} else {
		key, err = openssl.LoadPrivateKeyFromPEM(data)
	}
	if err != nil {
		return nil, err
	}

	return key, nil
}

func saveToPem(out io.Writer, key openssl.PrivateKey) error {

	data, err := key.MarshalPKCS1PrivateKeyPEM()
	if err != nil {
		return err
	}

	_, err = out.Write(data)

	return err
}

func loadPassphrase(passphrase_file string) (passphrase string, err error) {
	var fi *os.File
	var dat []byte
	if passphrase_file != "" {
		if passphrase_file == "-" {
			log.Debugf("Read passphrase from stdin.")
			fi, err = os.Open("/dev/stdin")
			if err != nil {
				log.Errorf("Failed to open stdin: %s", err)
				return "", err
			}
		} else {
			log.Debugf("Read passphrase from file.")
			fi, err = os.Open(passphrase_file)
			if err != nil {
				log.Errorf("Failed to open passphrase file: %s", err)
				return "", err
			}
		}
		defer fi.Close()
		dat, err = ioutil.ReadAll(fi)
		if err != nil {
			log.Errorf("Failed to read passphrase: %s", err)
			return "", err
		}
		passphrase = strings.TrimRight(string(dat), "\n")
	} else {
		passphrase = ""
	}

	return passphrase, nil
}
