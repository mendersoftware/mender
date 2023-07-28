// Copyright 2023 Northern.tech AS
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

package utils

import  (
	"strings"
)

const (
	pkcs11URIPrefix = "pkcs11:"
	tpmURIPrefix = "tpm2tss:"
)

func ispkcs11_keystring(key string) bool {
	return strings.HasPrefix(key, pkcs11URIPrefix)
}

func istpm2tss_keystring(key string) bool {
	return strings.HasPrefix(key, tpmURIPrefix)
}

// Function takes in a key string and based on the prefix outputs the correct format
func parsed_keystring(key string) string {
	// For tpm2tss keystring we pass in prefix + handle (i.e tpm2tss:0x81000000)
	// to identify it is of engine tpm2tss but the actual tpm2tss engine expects just the handle
	if istpm2tss_keystring(key) {
		return key[len(tpmURIPrefix):]
	}

	return key
}