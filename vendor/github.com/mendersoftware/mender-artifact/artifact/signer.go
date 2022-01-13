// Copyright 2021 Northern.tech AS
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

package artifact

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"math/big"

	"github.com/minio/sha256-simd"
	"github.com/pkg/errors"
)

// Signer is returning a signature of the provided message.
type Signer interface {
	Sign(message []byte) ([]byte, error)
}

// Verifier is verifying if provided message and signature matches.
type Verifier interface {
	Verify(message, sig []byte) error
}

// Crypto is an interface each specific signature algorithm must implement
// in order to be used with PKISigner.
type Crypto interface {
	Sign(message []byte, key interface{}) ([]byte, error)
	Verify(message, sig []byte, key interface{}) error
}

//
// RSA Crypto interface implementation
//
type RSA struct{}

func (r *RSA) Sign(message []byte, key interface{}) ([]byte, error) {
	var rsaKey *rsa.PrivateKey
	var ok bool

	// validate key
	if rsaKey, ok = key.(*rsa.PrivateKey); !ok {
		return nil, errors.New("signer: invalid private key")
	}

	h := sha256.Sum256(message)
	return rsa.SignPKCS1v15(rand.Reader, rsaKey, crypto.SHA256, h[:])
}

func (r *RSA) Verify(message, sig []byte, key interface{}) error {
	var rsaKey *rsa.PublicKey
	var ok bool

	// validate key
	if rsaKey, ok = key.(*rsa.PublicKey); !ok {
		return errors.New("signer: invalid rsa public key")
	}

	h := sha256.Sum256(message)
	return rsa.VerifyPKCS1v15(rsaKey, crypto.SHA256, h[:], sig)
}

//
// ECDSA Crypto interface implementation
//
const ecdsa256curveBits = 256
const ecdsa256keySize = 32

type ECDSA256 struct{}

func (e *ECDSA256) Sign(message []byte, key interface{}) ([]byte, error) {
	var ecdsaKey *ecdsa.PrivateKey
	var ok bool

	// validate key
	if ecdsaKey, ok = key.(*ecdsa.PrivateKey); !ok {
		return nil, errors.New("signer: invalid private key")
	}

	// calculate the hash of the message to be signed
	h := sha256.Sum256(message)
	r, s, err := ecdsa.Sign(rand.Reader, ecdsaKey, h[:])
	if err != nil {
		return nil, errors.Wrap(err, "signer: error signing message")
	}
	// check if the size of the curve matches expected one;
	// for now we are supporting only 256 bit ecdsa
	if ecdsaKey.Curve.Params().BitSize != ecdsa256curveBits {
		return nil, errors.New("signer: invalid ecdsa curve size")
	}
	return MarshalECDSASignature(r, s)
}

func (e *ECDSA256) Verify(message, sig []byte, key interface{}) error {
	var ecdsaKey *ecdsa.PublicKey
	var ok bool

	// validate key
	if ecdsaKey, ok = key.(*ecdsa.PublicKey); !ok {
		return errors.New("signer: invalid ecdsa public key")
	}

	r, s, err := UnmarshalECDSASignature(sig)
	if err != nil {
		return err
	}

	h := sha256.Sum256(message)

	ok = ecdsa.Verify(ecdsaKey, h[:], r, s)
	if !ok {
		return errors.New("signer: verification failed")
	}
	return nil

}

func MarshalECDSASignature(r, s *big.Int) ([]byte, error) {
	// we serialize the r and s into one array where the first
	// half is the r and the other one s;
	// as both values are ecdsa256curveBits size we need
	// 2*ecdsa256keySize size slice to store both

	// MEN-1740 In some cases the size of the r and s can be different
	// than expected ecdsa256keySize. In this case we need to make sure
	// we are serializing those using correct offset. We can use leading
	// zeros easily as this has no impact on serializing and deserializing.
	rSize := len(r.Bytes())
	sSize := len(s.Bytes())
	if rSize > ecdsa256keySize || sSize > ecdsa256keySize {
		return nil,
			errors.Errorf("signer: invalid size of ecdsa keys: r: %d; s: %d",
				rSize, sSize)
	}

	// if the keys are shorter than expected we need to use correct offset
	// while serializing
	rOffset := ecdsa256keySize - rSize
	sOffset := ecdsa256keySize - sSize

	serialized := make([]byte, 2*ecdsa256keySize)
	copy(serialized[rOffset:], r.Bytes())
	copy(serialized[ecdsa256keySize+sOffset:], s.Bytes())

	return serialized, nil
}

func UnmarshalECDSASignature(sig []byte) (r, s *big.Int, e error) {
	// check if the size of the key matches provided one
	if len(sig) != 2*ecdsa256keySize {
		return nil, nil, errors.Errorf("signer: invalid ecdsa key size: %d", len(sig))
	}

	// get the signature; see corresponding `Sign` function for more details
	// about serialization
	r = big.NewInt(0).SetBytes(sig[:ecdsa256keySize])
	s = big.NewInt(0).SetBytes(sig[ecdsa256keySize:])
	return r, s, nil
}

type SigningMethod struct {
	// Key can be private or public depending if we want to sign or verify message
	Key    interface{}
	Public []byte
	Method Crypto
}

// PKISigner implements public-key encryption and supports X.509-encodded keys.
// For now both RSA and 256 bits ECDSA are supported.
type PKISigner struct {
	signMethod, verifyMethod *SigningMethod
}

func NewPKISigner(privateKey []byte) (*PKISigner, error) {
	if len(privateKey) == 0 {
		return nil, errors.New("signer: missing key")
	}
	signMethod, err := GetKeyAndSignMethod(privateKey)
	if err != nil {
		return nil, err
	}
	pub, err := x509.ParsePKIXPublicKey(signMethod.Public)
	if err != nil {
		return nil, errors.Wrap(err, "failed to parse encoded public key")
	}
	return &PKISigner{
		signMethod: signMethod,
		verifyMethod: &SigningMethod{
			Key:    pub,
			Method: signMethod.Method,
		},
	}, nil
}

func NewPKIVerifier(publicKey []byte) (*PKISigner, error) {
	if len(publicKey) == 0 {
		return nil, errors.New("signer: missing key")
	}
	verifyMethod, err := GetKeyAndVerifyMethod(publicKey)
	if err != nil {
		return nil, err
	}
	return &PKISigner{
		verifyMethod: verifyMethod,
	}, nil
}

func (s *PKISigner) Sign(message []byte) ([]byte, error) {
	if s.signMethod == nil {
		return nil, errors.New("signer: only verification allowed with this signer")
	}
	sig, err := s.signMethod.Method.Sign(message, s.signMethod.Key)
	if err != nil {
		return nil, errors.Wrap(err, "signer: error signing image")
	}
	enc := make([]byte, base64.StdEncoding.EncodedLen(len(sig)))
	base64.StdEncoding.Encode(enc, sig)
	return enc, nil
}

func (s *PKISigner) Verify(message, sig []byte) error {
	dec := make([]byte, base64.StdEncoding.DecodedLen(len(sig)))
	decLen, err := base64.StdEncoding.Decode(dec, sig)
	if err != nil {
		return errors.Wrap(err, "signer: error decoding signature")
	}

	if s.verifyMethod == nil {
		return errors.New("verifyMethod is nil")
	}
	if s.verifyMethod.Method == nil {
		return errors.New("verifyMethod.Method is nil")
	}

	return s.verifyMethod.Method.Verify(message, dec[:decLen], s.verifyMethod.Key)
}

func GetPublic(private []byte) ([]byte, error) {
	sm, err := GetKeyAndSignMethod(private)
	if err != nil {
		return nil, errors.Wrap(err, "signer: error parsing private key")
	}
	return sm.Public, nil
}

func GetKeyAndVerifyMethod(keyPEM []byte) (*SigningMethod, error) {
	block, _ := pem.Decode(keyPEM)
	if block == nil {
		return nil, errors.New("signer: failed to parse public key")
	}

	pub, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, errors.Wrap(err, "failed to parse encoded public key")
	}
	switch pub := pub.(type) {
	case *rsa.PublicKey:
		return &SigningMethod{Key: pub, Method: new(RSA)}, nil
	case *ecdsa.PublicKey:
		return &SigningMethod{Key: pub, Method: new(ECDSA256)}, nil
	default:
		return nil, errors.Errorf("unsupported public key type: %v", pub)
	}
}

func GetKeyAndSignMethod(keyPEM []byte) (*SigningMethod, error) {
	block, _ := pem.Decode(keyPEM)
	if block == nil {
		return nil, errors.New("signer: failed to parse private key")
	}
	rsaKey, err := x509.ParsePKCS1PrivateKey(block.Bytes)
	if err == nil {
		pub, keyErr := x509.MarshalPKIXPublicKey(rsaKey.Public())
		if keyErr != nil {
			return nil, errors.Wrap(err, "signer: can not extract public RSA key")
		}
		return &SigningMethod{Key: rsaKey, Public: pub, Method: new(RSA)}, nil
	}
	ecdsaKey, err := x509.ParseECPrivateKey(block.Bytes)
	if err == nil {
		pub, keyErr := x509.MarshalPKIXPublicKey(ecdsaKey.Public())
		if keyErr != nil {
			return nil, errors.Wrap(err, "signer: can not extract public ECDSA key")
		}
		return &SigningMethod{Key: ecdsaKey, Public: pub, Method: new(ECDSA256)}, nil
	}
	return nil, errors.Wrap(err, "signer: unsupported private key type or error occured")
}
