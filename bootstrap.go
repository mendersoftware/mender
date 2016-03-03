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

import "errors"
import "github.com/mendersoftware/log"
import "fmt"
import "io/ioutil"
import "net/http"
import "crypto/tls"
import "crypto/x509"

//TODO: this will be hardcoded for now but should be configurable in future
const (
	certFile   = "/data/certfile"
	certKey    = "/data/certkey"
	serverCert = "/data/servercert"
)

func validateBootstrap(args *authCredsType) error {

	if *args.bootstrapServer == "" {
		panic("trying to validate bootstrap parameters while not performing bootstrap")
	}

	args.trustedCerts = *x509.NewCertPool()
	if *args.serverCert != "" {
		CertPoolAppendCertsFromFile(&args.trustedCerts, *args.serverCert)
	}

	if len(args.trustedCerts.Subjects()) == 0 {
		return errors.New("No server certificate is trusted," +
			" use -trusted-certs with a proper certificate")
	}

	if clientCert, err := tls.LoadX509KeyPair(*args.certFile, *args.certKey); err != nil {
		return errors.New("Failed to load certificate and key from files: " +
			*args.certFile + " " + *args.certKey)
	} else {
		args.clientCert = clientCert
	}

	return nil
}

func doBootstrap(serverHostName string, trustedCerts x509.CertPool,
	clientCert tls.Certificate) error {

	tlsConf := tls.Config{
		RootCAs:      &trustedCerts,
		Certificates: []tls.Certificate{clientCert},
		// InsecureSkipVerify : true,
	}

	transport := http.Transport{
		TLSClientConfig: &tlsConf,
	}

	httpClient := http.Client{
		Transport: &transport,
	}

	serverURL := "https://" + serverHostName + "/bootstrap"
	fmt.Println("Sending HTTP GET to: ", serverURL)

	response, err := httpClient.Get(serverURL)
	if err != nil {
		fmt.Println("HTTP GET failed:", err)
		return nil // TODO
	}
	defer response.Body.Close()

	fmt.Println("Received headers:", response.Header)
	respData, err := ioutil.ReadAll(response.Body)
	if err != nil {
		fmt.Println("Received error:", err)
	} else {
		fmt.Println("Received data:", string(respData))
	}

	return nil
}
