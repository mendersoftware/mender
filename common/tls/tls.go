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
	"fmt"
	"io/ioutil"
	"path"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	"github.com/mendersoftware/openssl"
)

const (
	errMissingServerCertF = "IGNORING ERROR: The client server-certificate can not be " +
		"loaded: (%s). The client will continue running, but may not be able to " +
		"communicate with the server. If this is not your intention please add a valid " +
		"server certificate"
	errMissingCerts = "No trusted certificates. The client will continue running, but will " +
		"not be able to communicate with the server. Either specify ServerCertificate in " +
		"mender.conf, or make sure that CA certificates are installed on the system"
	pkcs11URIPrefix = "pkcs11:"
)

var (
	// 	                  http.Client.Timeout
	// +--------------------------------------------------------+
	// +--------+  +---------+  +-------+  +--------+  +--------+
	// |  Dial  |  |   TLS   |  |Request|  |Response|  |Response|
	// |        |  |handshake|  |       |  |headers |  |body    |
	// +--------+  +---------+  +-------+  +--------+  +--------+
	// +--------+  +---------+             +--------+
	//  Dial        TLS                     Response
	//  timeout     handshake               header
	//  timeout                             timeout
	//
	//  It covers the entire exchange, from Dial (if a connection is not reused)
	// to reading the body. This is to timeout long lasting connections.
	//
	// 4 hours should be enough to download a 2GB image file with the
	// average download speed ~1 mbps
	DefaultClientReadingTimeout = 4 * time.Hour

	// connection keepalive options
	ConnectionKeepaliveTime = 10 * time.Second
)

type Config struct {
	ServerCert string
	*HttpsClient
	NoVerify bool
}

// HttpsClient holds the configuration for the client side mTLS configuration
// NOTE: Careful when changing this, the struct is exposed directly in the
// 'mender.conf' file.
type HttpsClient struct {
	Certificate string `json:",omitempty"`
	Key         string `json:",omitempty"`
	SSLEngine   string `json:",omitempty"`
}

// Security structure holds the configuration for the client
// Added for MEN-3924 in order to provide a way to specify PKI params
// outside HttpsClient.
// NOTE: Careful when changing this, the struct is exposed directly in the
// 'mender.conf' file.
type Security struct {
	AuthPrivateKey string `json:",omitempty"`
	SSLEngine      string `json:",omitempty"`
}

func (h *HttpsClient) Validate() {
	if h == nil {
		return
	}
	if h.Certificate != "" || h.Key != "" {
		if h.Certificate == "" {
			log.Error("The 'Key' field is set in the mTLS configuration, but no 'Certificate' is given. Both need to be present in order for mTLS to function")
		}
		if h.Key == "" {
			log.Error("The 'Certificate' field is set in the mTLS configuration, but no 'Key' is given. Both need to be present in order for mTLS to function")
		} else if strings.HasPrefix(h.Key, pkcs11URIPrefix) && len(h.SSLEngine) == 0 {
			log.Errorf("The 'Key' field is set to be loaded from %s, but no 'SSLEngine' is given. Both need to be present in order for loading of the key to function",
				pkcs11URIPrefix)
		}
	}
}

// nrOfSystemCertsFound simply returns the number of certificates found in
// 'certDir'. The only reason this is needed, is so that the user can be
// notified if the system certificate directory is empty, since this is not done
// by the OpenSSL wrapper, in the same manner it was done by the Go standard
// library. OpenSSL loads the trust chain on a dial. This system cert, and
// server cert path are set by the 'SetDefaultVerifyPaths' and
// 'LoadVerifyLocations' respectively.
func nrOfSystemCertsFound(certDir string) (int, error) {
	sysCertsFound := 0
	files, err := ioutil.ReadDir(certDir)
	if err != nil {
		return 0, fmt.Errorf("Failed to read the OpenSSL default directory (%s). Err %v", certDir, err.Error())
	}
	for _, certFile := range files {
		certBytes, err := ioutil.ReadFile(path.Join(certDir, certFile.Name()))
		if err != nil {
			log.Debugf("Failed to read the certificate file for the HttpsClient. Err %v", err.Error())
			continue
		}

		certs := openssl.SplitPEM(certBytes)
		if len(certs) == 0 {
			log.Debugf("No PEM certificate found in '%s'", certFile)
			continue
		}
		first, certs := certs[0], certs[1:]
		_, err = openssl.LoadCertificateFromPEM(first)
		if err != nil {
			log.Debug(err.Error())
		} else {
			sysCertsFound += 1
		}
	}
	return sysCertsFound, nil
}

func loadServerTrust(ctx *openssl.Ctx, conf *Config) (*openssl.Ctx, error) {
	defaultCertDir, err := openssl.GetDefaultCertificateDirectory()
	if err != nil {
		return ctx, errors.Wrap(err, "Failed to get the default OpenSSL certificate directory. Please verify the OpenSSL setup")
	}
	sysCertsFound, err := nrOfSystemCertsFound(defaultCertDir)
	if err != nil {
		log.Warnf("Failed to list the system certificates with error: %s", err.Error())
	}

	// Set the default system certificate path for this OpenSSL context
	err = ctx.SetDefaultVerifyPaths()
	if err != nil {
		return ctx, fmt.Errorf("Failed to set the default OpenSSL directory. OpenSSL error code: %s", err.Error())
	}
	// Load the server certificate into the OpenSSL context
	err = ctx.LoadVerifyLocations(conf.ServerCert, "")
	if err != nil {
		if strings.Contains(err.Error(), "No such file or directory") {
			log.Warnf(errMissingServerCertF, conf.ServerCert)
		} else {
			log.Errorf("Failed to Load the Server certificate. Err %s", err.Error())
		}
		// If no system certificates, nor a server certificate is found,
		// warn the user, as this is a pretty common error.
		if sysCertsFound == 0 {
			log.Error(errMissingCerts)
		}
	}
	return ctx, err
}

func loadPrivateKey(keyFile string, engineId string) (key openssl.PrivateKey, err error) {
	if strings.HasPrefix(keyFile, pkcs11URIPrefix) {
		engine, err := openssl.EngineById(engineId)
		if err != nil {
			log.Errorf("Failed to Load '%s' engine. Err %s",
				engineId, err.Error())
			return nil, err
		}

		key, err = openssl.EngineLoadPrivateKey(engine, keyFile)
		if err != nil {
			log.Errorf("Failed to Load private key from engine '%s'. Err %s",
				engineId, err.Error())
			return nil, err
		}
		log.Infof("loaded private key: '%s...' from '%s'.", pkcs11URIPrefix, engineId)
	} else {
		keyBytes, err := ioutil.ReadFile(keyFile)
		if err != nil {
			return nil, errors.Wrap(err, "Private key file from the HttpsClient configuration not found")
		}

		key, err = openssl.LoadPrivateKeyFromPEM(keyBytes)
		if err != nil {
			return nil, err
		}
	}

	return key, nil
}

func loadClientTrust(ctx *openssl.Ctx, conf *Config) (*openssl.Ctx, error) {

	if conf.HttpsClient == nil {
		return ctx, errors.New("Empty HttpsClient config given")

	}
	certFile := conf.HttpsClient.Certificate

	certBytes, err := ioutil.ReadFile(certFile)
	if err != nil {
		return ctx, errors.Wrap(err, "Failed to read the certificate file for the HttpsClient")
	}

	certs := openssl.SplitPEM(certBytes)
	if len(certs) == 0 {
		return ctx, fmt.Errorf("No PEM certificate found in '%s'", certFile)
	}
	first, certs := certs[0], certs[1:]
	cert, err := openssl.LoadCertificateFromPEM(first)
	if err != nil {
		return ctx, err
	}

	err = ctx.UseCertificate(cert)
	if err != nil {
		return ctx, err
	}

	for _, pem := range certs {
		cert, err := openssl.LoadCertificateFromPEM(pem)
		if err != nil {
			return ctx, err
		}
		err = ctx.AddChainCertificate(cert)
		if err != nil {
			return ctx, err
		}
	}

	key, err := loadPrivateKey(conf.HttpsClient.Key, conf.HttpsClient.SSLEngine)
	if err != nil {
		return ctx, err
	}

	err = ctx.UsePrivateKey(key)
	if err != nil {
		return ctx, err
	}

	return ctx, nil
}

func dialOpenSSL(ctx *openssl.Ctx, conf *Config, network string, addr string) (net.Conn, error) {

	flags := openssl.DialFlags(0)

	if conf.NoVerify {
		flags = openssl.InsecureSkipHostVerification
	}

	conn, err := openssl.Dial("tcp", addr, ctx, flags)
	if err != nil {
		return nil, err
	}

	if conf.NoVerify {
		return conn, nil
	}
	v := conn.VerifyResult()
	if v != openssl.Ok {
		if v == openssl.CertHasExpired {
			return nil, errors.Errorf("certificate has expired, "+
				"openssl verify rc: %d server cert file: %s", v, conf.ServerCert)
		}
		if v == openssl.DepthZeroSelfSignedCert {
			return nil, errors.Errorf("depth zero self-signed certificate, "+
				"openssl verify rc: %d server cert file: %s", v, conf.ServerCert)
		}
		if v == openssl.EndEntityKeyTooSmall {
			return nil, errors.Errorf("end entity key too short, "+
				"openssl verify rc: %d server cert file: %s", v, conf.ServerCert)
		}
		if v == openssl.UnableToGetIssuerCertLocally {
			return nil, errors.Errorf("certificate signed by unknown authority, "+
				"openssl verify rc: %d server cert file: %s", v, conf.ServerCert)
		}
		return nil, errors.Errorf("not a valid certificate, "+
			"openssl verify rc: %d server cert file: %s", v, conf.ServerCert)
	}
	return conn, err
}

func newHttpsClient(conf Config) (*http.Client, error) {
	client := &http.Client{}

	ctx, err := openssl.NewCtx()
	if err != nil {
		return nil, err
	}

	ctx, err = loadServerTrust(ctx, &conf)
	if err != nil {
		log.Warn(errors.Wrap(err, "Failed to load the server TLS certificate settings"))
	}

	if conf.HttpsClient != nil {
		ctx, err = loadClientTrust(ctx, &conf)
		if err != nil {
			log.Warn(errors.Wrap(err, "Failed to load the client TLS certificate settings"))
		}
	}

	if conf.NoVerify {
		log.Warn("certificate verification skipped..")
	}
	transport := http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialTLS: func(network string, addr string) (net.Conn, error) {
			return dialOpenSSL(ctx, &conf, network, addr)
		},
	}

	client.Transport = &transport
	return client, nil
}

func NewHttpOrHttpsClient(conf Config) (*http.Client, error) {
	var client *http.Client
	var err error
	if conf == (Config{}) {
		client = &http.Client{}
	} else {
		client, err = newHttpsClient(conf)
	}
	if err != nil {
		return nil, err
	}

	if client.Transport == nil {
		client.Transport = &http.Transport{
			Proxy: http.ProxyFromEnvironment,
		}
	}
	// set connection timeout
	client.Timeout = DefaultClientReadingTimeout

	transport := client.Transport.(*http.Transport)
	//set keepalive options
	transport.DialContext = (&net.Dialer{
		KeepAlive: ConnectionKeepaliveTime,
	}).DialContext

	return client, nil
}
