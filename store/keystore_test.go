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
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"testing"

	"github.com/stretchr/testify/assert"
)

const (
	// malformed key, sequence MIIEogIBAAKCAQEAm38 changed to
	// MIIEogIBAAKCAQEAm44, this should give invalid modulus error
	//
	// since openssl does not consider this key bad,
	// change MIIEogIBAAKCAQ to MIIEogIBAAKCAZ
	// this damages the whole key structure.
	//
	// -> openssl asn1parse -in badkey
	//     0:d=0  hl=4 l=1186 cons: SEQUENCE
	//     4:d=1  hl=2 l=   1 prim: INTEGER           :00
	//     7:d=1  hl=4 l= 401 prim: INTEGER           :9B8E2ABA900A4A1E3695A688FA73B864BAE4BF46434ABAD80C135F15BE773BB9D8538DA0122D4BB66EA8FF5E157E3
	//   412:d=1  hl=2 l=  86 cons: priv [ 25 ]
	// Error in encoding
	// 140556877706368:error:0D07207B:asn1 encoding routines:ASN1_get_object:header too long:../crypto/asn1/asn1_lib.c:101:
	badPrivKey = `
-----BEGIN RSA PRIVATE KEY-----
MIIEogIBAAKCAZEAm44qupAKSh42laaI+nO4ZLrkv0ZDSrrYDBNfFb53O7nYU42g
Ei1Ltm6o/14VfrSy/7bkjNcBHQLEni4wRdM042gOWYxXFqNMfEnL7APzWCvTFlVo
MGa4++L25PPLl+1BqQFfNuwgW/1ZM3pVyWCCQ+wgw2MCqjPMbqE5txQWfDV7dVfa
ByH1NtjhboSQB89VTmwYAbbleFRAlV9J6IWkNEsfBpDGazqUfwJJv8ToIvJNFxIw
P4LmhmcfXxFKkMsEvdvt6BiR7yiIsaoJ9ZODbnrK+VB6g+5jPJtYsApjf8MKCELe
wtTiPLV5/VcpOVZ9WwnFIQK/4yb4LWrGKcquawIDAQABAoIBAGG3w8FkPaMgY4se
EdzalhlvPctaO3Wd/6FvFwUSIdn9y42OZfamUns+BaQdmwJ6Sjba17wObZuunqMN
QbbPqN/0B3iM8jm+u5UrxyP1w5o4SDozx/sKwttAYYm2D87VAbtUqmJYd2l3x/PK
wFiB9rr6jAhdk1IkpScs2JlN3WeGNczBPhiTA/lWJ8df4Kqb1k58BhRqhST6mUcj
sX9jpjqaXtKLBOfdfxtAVH2imCwrqCPAL3GLOd1M4sE50XBdbCKQt30yQWQzgKeu
RnEb8W3OPPOrVK7ponudrc2SxqfViloQEdrdnhmwz56xmmVRZKagrKiUYmSqT9et
qEidomkCgYEAzIL78pWhw4GLN8PejfkZ5QXG6Qp1cNNLIWXaiZQaULhG/Byl6Uou
+b7yz/xXu+VOaDIsJhIzQQ5KxjeUdqWFSOIZr5XBmDippN4OO7ycK3bA96wTD0kO
Rqnf0BT844FWJ7EnrElDRWLOXxFES7LzFyV+02NX5kMwN76iUr57jr0CgYEAwqUd
QvkUjpiEVjJhSQiFVapc1v8PH1Q2Y+p8Rm4bw/o4GsC7bxvyIdDtdTauKSi/8cCQ
nTIy5taLJtlAVLqj8cZbxlTQs/41aciJ4m2JmW9D2y8ai7TQ7H+Jd/B8btP9DBz/
cAsXalhu6dhH8SSG9EKM7n0I3w0N0Mlmnqer+EcCgYBOFRyYzCSM/qLm0bPhROBs
Hr6JL2MThrjCsZ60tIUvmIwRqeZ2oco5tHwEiPX+WViMU8ujZYOILSrDb2kRu7Sd
1SW1cloOAmRS/C03BZYiyh528Y39Ygk/VZCMY9cCDdmVIgBhuT8j+MuOZItM07AY
gEph7yYaVkDMp85WBUAriQKBgAGi778LZw/X2mz7GXRKvQw+VW99T3w88gQfCZJy
BIu+Q9B9xFWnz35XSlfM8OPpsstuigi4TlNAhIT8GJ1dwFkdCNJ/Dg4lWf+crwQX
VavTkqd6GugHyiXi4J4AiJtJ7vu2FrOzdCvxuGUA64Hsg7H0CUlMBdISQwZ5WwKE
eF6rAoGAA3FBdP0qsYITb3/zHUP88XYIR88iAOSkPOGK6UsxXlLUKCiMhLygjaFa
c0Z2UxFtksT1vezCXMe6/b7+S/S+rN2FvlGen+jgz+41G4ARcyGeTDCxnKFkuhVk
AuMObwrNlzbL4utcxhadX27MmpV9z4GGIJGYkNo4gFE9hNWGmG4=
-----END RSA PRIVATE KEY-----
`
)

func TestKeystore(t *testing.T) {
	ms := NewMemStore()

	k := NewKeystore(nil, "", false)
	assert.Nil(t, k)

	var err error

	k = NewKeystore(ms, "foo", false)

	// keystore has no keys, save should fail
	err = k.Save()
	assert.Error(t, err)
	assert.True(t, IsNoKeys(err))

	// try to load from an entry that does not exist
	err = k.Load()
	assert.Error(t, err)
	assert.True(t, IsNoKeys(err))
	assert.Nil(t, k.Private())

	// make our store inaccessible, should yield error other than IsNoKeys()
	ms.Disable(true)
	err = k.Load()
	assert.Error(t, err)
	assert.False(t, IsNoKeys(err))
	assert.Nil(t, k.Private())
	ms.Disable(false)

	// load some bogus data into store
	ms.WriteAll("foo", []byte(""))

	// try using temp file, this time we should get unmarshal/load
	// error
	err = k.Load()
	assert.Error(t, err)
	assert.False(t, IsNoKeys(err))
	assert.Nil(t, k.Private())

	// not changing random source, so this is not expected to fail
	assert.NoError(t, k.Generate())

	assert.NotNil(t, k.Private())

	// make the store read only
	ms.ReadOnly(true)
	assert.Error(t, k.Save())
	ms.ReadOnly(false)

	// try again
	assert.NoError(t, k.Save())

	// we should be able to load a saved key
	assert.NoError(t, k.Load())

	// check public key
	pubkey := k.Public()
	assert.NotNil(t, pubkey)

	// serialize to PEM
	expectpubarray, err := k.private.MarshalPKIXPublicKeyPEM()
	assert.NoError(t, err)
	expectedaspem := string(expectpubarray)

	aspem, err := k.PublicPEM()
	assert.NoError(t, err)
	assert.Equal(t, expectedaspem, aspem)

	tosigndata := []byte("foobar")
	s, err := k.Sign(tosigndata)
	assert.NoError(t, err)
	// generate hash of data for verification
	h := crypto.SHA256.New()
	h.Write(tosigndata)
	hashed := h.Sum(nil)

	//generate pubkey for golang stdlib
	block, _ := pem.Decode([]byte(expectedaspem))
	assert.NotNil(t, block)
	gokey, err := x509.ParsePKIXPublicKey(block.Bytes)
	assert.NoError(t, err)
	rsagokey, ok := gokey.(*rsa.PublicKey)
	assert.True(t, ok)

	err = rsa.VerifyPKCS1v15(rsagokey, crypto.SHA256, hashed, s)
	// signature should be valid
	assert.NoError(t, err)
}

func TestKeystoreLoadPem(t *testing.T) {
	// this should fail
	nk, err := loadFromPem(bytes.NewBufferString(badPrivKey))
	assert.Nil(t, nk)
	assert.Error(t, err)
}
