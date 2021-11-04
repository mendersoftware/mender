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
package tls

import (
	"io/ioutil"
	"os"
	"path"
	"testing"

	"github.com/mendersoftware/openssl"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadingTrust(t *testing.T) {
	t.Run("Test loading server trust", func(t *testing.T) {
		ctx, err := openssl.NewCtx()
		assert.NoError(t, err)

		ctx, err = loadServerTrust(ctx, &Config{
			ServerCert:  "missing.crt",
			HttpsClient: nil,
			NoVerify:    false,
		})
		assert.Error(t, err)

		ctx, err = loadServerTrust(ctx, &Config{
			ServerCert:  "testdata/server.crt",
			HttpsClient: nil,
			NoVerify:    false,
		})
		assert.NoError(t, err)
	})
	t.Run("Test loading client trust", func(t *testing.T) {

		tests := map[string]struct {
			conf       Config
			assertFunc func(t assert.TestingT, err error, msgAndArgs ...interface{}) bool
		}{
			"No HttpsClient given": {
				conf: Config{
					HttpsClient: nil,
					NoVerify:    false,
				},
				assertFunc: func(t assert.TestingT, err error, msgAndArgs ...interface{}) bool {
					return assert.Error(t, err) &&
						assert.Contains(t, err.Error(), "Empty HttpsClient config given")
				},
			},
			"Missing certificate": {
				conf: Config{
					HttpsClient: &HttpsClient{
						Certificate: "missing.crt",
						Key:         "foobar",
					},
					NoVerify: false,
				},
				assertFunc: func(t assert.TestingT, err error, msgAndArgs ...interface{}) bool {
					return assert.Error(t, err) &&
						assert.Contains(t, err.Error(), "Failed to read the certificate")
				},
			},
			"No PEM certificate found in file": {
				conf: Config{
					HttpsClient: &HttpsClient{
						Certificate: "test_server/https_server.go",
						Key:         "foobar",
					},
					NoVerify: false,
				},
				assertFunc: func(t assert.TestingT, err error, msgAndArgs ...interface{}) bool {
					return assert.Error(t, err) &&
						assert.Contains(t, err.Error(), "No PEM certificate found in")
				},
			},
			"Certificate chain loading": {
				conf: Config{
					HttpsClient: &HttpsClient{
						Certificate: "testdata/chain-cert.crt",
						Key:         "testdata/client-cert.key",
					},
				},
				assertFunc: func(t assert.TestingT, err error, msgAndArgs ...interface{}) bool {
					return assert.NoError(t, err)
				},
			},
			"Missing Private key file": {
				conf: Config{
					HttpsClient: &HttpsClient{
						Certificate: "testdata/client.crt",
						Key:         "non-existing.key",
					},
					NoVerify: false,
				},
				assertFunc: func(t assert.TestingT, err error, msgAndArgs ...interface{}) bool {
					return assert.Error(t, err) &&
						assert.Contains(t, err.Error(), "Private key file from the ")
				},
			},
			"Correct certificate, wrong key": {
				conf: Config{
					HttpsClient: &HttpsClient{
						Certificate: "testdata/client.crt",
						Key:         "testdata/wrong.key",
					},
					NoVerify: false,
				},
				assertFunc: func(t assert.TestingT, err error, msgAndArgs ...interface{}) bool {
					return assert.Error(t, err) &&
						assert.Contains(t, err.Error(), "key values mismatch")
				},
			},
			"Correct certificate, correct key": {
				conf: Config{
					HttpsClient: &HttpsClient{
						Certificate: "testdata/client.crt",
						Key:         "testdata/client-cert.key",
					},
					NoVerify: false,
				},
				assertFunc: assert.NoError,
			},
		}

		for _, test := range tests {
			ctx, err := openssl.NewCtx()
			assert.NoError(t, err)

			ctx, err = loadClientTrust(ctx, &test.conf)
			test.assertFunc(t, err)
		}
	})
}

func TestListSystemCertsFound(t *testing.T) {
	// Setup tmpdir with two certificates and one private key
	tdir, err := ioutil.TempDir("", "TestListSystemCertsFound")
	require.NoError(t, err)
	wd, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Symlink(path.Join(wd, "../../common/tls/testdata/server.crt"), tdir+"/server.crt"))
	require.NoError(t, os.Symlink(path.Join(wd, "../../common/tls/testdata/chain-cert.crt"), tdir+"/chain-cert.crt"))
	require.NoError(t, os.Symlink(path.Join(wd, "../../common/tls/testdata/wrong.key"), tdir+"/wrong.key"))
	defer os.Remove(tdir)
	tests := map[string]struct {
		certDir              string
		assertFunc           func(t assert.TestingT, err error, msgAndArgs ...interface{}) bool
		certificatesExpected int
	}{
		"No such directory": {
			certDir:              "/i/do/not/exist",
			assertFunc:           assert.Error,
			certificatesExpected: 0,
		},
		"No system certificates found": {
			certDir:              "..", // There should be no certificates in the root of our repo
			assertFunc:           assert.NoError,
			certificatesExpected: 0,
		},
		"System certificates found": {
			certDir:              tdir,
			assertFunc:           assert.NoError,
			certificatesExpected: 2,
		},
	}

	for name, test := range tests {
		sysCerts, err := nrOfSystemCertsFound(test.certDir)
		test.assertFunc(t, err)
		assert.Equal(t, test.certificatesExpected, sysCerts, name)
	}
}

func TestHttpClient(t *testing.T) {
	cl, _ := NewHttpOrHttpsClient(
		Config{ServerCert: "testdata/server.crt"},
	)
	assert.NotNil(t, cl)

	// no https config, we should obtain a httpClient
	cl, _ = NewHttpOrHttpsClient(Config{})
	assert.NotNil(t, cl)

	// missing cert in config should still yield usable client
	cl, err := NewHttpOrHttpsClient(
		Config{ServerCert: "testdata/missing.crt"},
	)
	assert.NotNil(t, cl)
	assert.NoError(t, err)
}
