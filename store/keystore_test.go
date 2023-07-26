// Copyright 2023 Northern.tech AS
//
//	Licensed under the Apache License, Version 2.0 (the "License");
//	you may not use this file except in compliance with the License.
//	You may obtain a copy of the License at
//
//	    http://www.apache.org/licenses/LICENSE-2.0
//
//	Unless required by applicable law or agreed to in writing, software
//	distributed under the License is distributed on an "AS IS" BASIS,
//	WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//	See the License for the specific language governing permissions and
//	limitations under the License.
package store

import (
	"bytes"
	"crypto"
	"crypto/ed25519"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"os"
	"testing"

	"github.com/mendersoftware/openssl"
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

const (
	encryptedPrivKey = `
-----BEGIN RSA PRIVATE KEY-----
Proc-Type: 4,ENCRYPTED
DEK-Info: AES-128-CBC,6CBCB52903E5ED7C10CEC8E73B10F9FC

ynJjv41txYeS8z05tpNvOKZ9uuYiJyJYn2/nZXvXvhWZHco0vu+GTU7lVihtcosJ
CjlXS1hCRRnPXJSmOJUowAjG0v4QyjLL5Wb7tBNlk/XDLcsxhkaWuaAMgHYFf7qB
5QTekYebQWgMZuIjMn5U+JYL9ebV5pjmvX17O8lm2sWNJXvAv0oZPtOj/VtnXvJ6
2QMSzSTTaVWyvFN+oQpyJrUsIbgV44wdRBlbFxF9zTaXy9nh+x7oYyN1WFJ2vz/F
YGSTV4no0HWi1FQ+S1st2tD6TToSG5LAmoI5tr1eXzBYvCw9ZZX8Riklp29+gZTV
EfLpyWavuJwKZ+L/7yUVqmymC9d7KS0PuLnLPTAtnKF4mjIsdWuO3PJSTD0qC3A9
9UqeEOVIHQQyc1QU/ghhZ0WAQqV6M0fZmAlV7ZvvhTITCj0DbFcrx0UsWhOZTuny
lOKSHq7JTiuAOf0i7LLMpyp9v2HudgGkK9V0hwTBv1c2ZJurxD3waLjyzvI+mzmJ
auqgw6Pbc6u5cqSGhDCTcCrxN2EsdV9nNus8qRYjd2eZSnpk9TkoZAVEdg91JGrJ
ZmOiZpvAJ9L0xJR8JmPWil6mKQn81BL0OSeJ7dH8I4BK2G6Eo5DB/akHTHq/gcMM
8HBwHNNJt4twmk68INdo0lsyC5HlRmjsHmKrtd7mogS8khCwSm/absgZUC1d2W4b
M/YdsKwEx/UErQ+wGTV/r55sv4EHt4yeUWYtlmEvBtq5Xlgnivbv6M5lJLJougn0
cqhTwSBOWjpYxb7yFPzkkG8dgEc5YCeFHiFZKNFE5Bw3KXuYwahfwk2V8TBdeVib
Kkta13k8PSpGvaErUZYpOvdz2F7PW9nx8mjhbvGP+z/V+6hSW3tnERil8c/WH+KW
Vfg1IL+eIDPkcEt7YrWZtMrOfLChYg9AxPUVDuNyO4eM5rEBB2Bxwg/A0xo7JYjY
34XQZVWf7JCN5/JyYROfOg1O+CY1eBSQ9fZ3i1ek1cHE+Ww1/dSHdHhgZ9J7FjxM
loWa3CZtGjm7eSaadvbmtLW7lq6st583VTx1sQacPDufjw7Srh5g2CoqVBHVZ5VS
2PuRfgM4EMZgpjOBs6hzcAj1tB7X/QFML5HzO1/W7L7NzV6lLkXqmEAdBIdgzGib
zhWQYm5GhcfPLg2yUwj2Xez5zOQfGLkhiW5+3EIXnOzzJidSt7jh8TVi8l5z7Vzh
rfjwTXTQ+3UEFRLvUCxS5j91oyeBuqo3qTAG56z7TLMICQCI+SoBAm8njLcgp+tM
DyqOQNp6V2De4Bu5yB1Ji4K8RV1+mx/qgOOemCHZ+ZaMpd59uLaLTpQv9heRIeEn
dPNbW0zzjTX0We6ojuOXZVRoj+o7rqboGoLMZmVOe60PQM35tQuNSo8k76tNqo5V
s6uVPfQS5FyO3+Ozc4FYS/kxlprMnzC9ZzYUeTjL4I1lU8MHAVqei3O7XE4nPadn
aw6vmU1/a31eR41KBVfcxNIsippaM9N6hkTAq9gK49G6iRGHEQ5EggkUq5PX/R75
Zjl01sTx+QGssG7CDbSoIAohXs7lWag1wdHhNRRzgoj73dc8rfdSW2lY2p/hhaSX
-----END RSA PRIVATE KEY-----
`
)

func TestKeystore(t *testing.T) {
	ms := NewMemStore()
	keyPassphrase := ""

	k := NewKeystore(nil, "", "", false, keyPassphrase)
	assert.Nil(t, k)

	var err error

	k = NewKeystore(ms, "foo", "", false, keyPassphrase)

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

func TestSignED25519(t *testing.T) {

	ms := NewMemStore()
	tosigndata := []byte("foobar")

	// Test signing with ED25519
	key, err := openssl.GenerateED25519Key()
	assert.NoError(t, err)
	k := &Keystore{
		store:   ms,
		private: key,
		keyName: "foobar",
	}
	assert.NotNil(t, k)
	assert.True(
		t,
		k.private.KeyType() == openssl.KeyTypeED25519,
		"KeyType: %s",
		k.private.KeyType(),
	)
	_, err = k.Sign(tosigndata)
	assert.NoError(t, err)

	// serialize to PEM
	expectpubarray, err := k.private.MarshalPKIXPublicKeyPEM()
	assert.NoError(t, err)
	expectedaspem := string(expectpubarray)

	aspem, err := k.PublicPEM()
	assert.NoError(t, err)
	assert.Equal(t, expectedaspem, aspem)

	// Sign the data with the ED25519 key
	s, err := k.Sign(tosigndata)
	assert.NoError(t, err)

	//generate pubkey for golang stdlib
	block, _ := pem.Decode([]byte(expectedaspem))
	assert.NotNil(t, block)
	gokey, err := x509.ParsePKIXPublicKey(block.Bytes)
	assert.NoError(t, err)
	ed25519gokey, ok := gokey.(ed25519.PublicKey)
	assert.True(t, ok)

	// Verify the signature
	ok = ed25519.Verify(ed25519gokey, tosigndata, s)
	assert.True(t, ok)
}

func TestKeystoreLoadPemFail(t *testing.T) {
	// this should fail
	nk, err := loadFromPem(bytes.NewBufferString(badPrivKey), "")
	assert.Nil(t, nk)
	assert.Error(t, err)
}

func TestKeystoreLoadPemWithSecret(t *testing.T) {
	nk, err := loadFromPem(bytes.NewBufferString(encryptedPrivKey), "verysecure")
	assert.NotNil(t, nk)
	assert.Nil(t, err)
}

func TestKeystoreLoadPemWithSecretFail(t *testing.T) {
	// this should fail
	nk, err := loadFromPem(bytes.NewBufferString(badPrivKey), "verysecure")
	assert.Nil(t, nk)
	assert.Error(t, err)
}
func TestKeystoreLoadPemWithWrongSecret(t *testing.T) {
	nk, err := loadFromPem(bytes.NewBufferString(encryptedPrivKey), "wrongsecret")
	assert.Nil(t, nk)
	assert.Error(t, err)
}

func TestKeystoreLoadPassphraseFromFile(t *testing.T) {
	// Create temporary passphrase file
	passphraseFile, _ := os.Create("passphrase_file")
	defer os.Remove("passphrase_file")

	// write passphrase into file
	passphraseFile.WriteString("verysecure")

	nk, err := loadPassphrase("passphrase_file")
	assert.Equal(t, nk, "verysecure")
	assert.Nil(t, err)
}

func TestKeystoreLoadPassphraseFromFileOpenFail(t *testing.T) {
	// this should fail because file does not exists
	nk, err := loadPassphrase("no_passphrase_file")
	assert.Equal(t, nk, "")
	assert.Error(t, err)
}
