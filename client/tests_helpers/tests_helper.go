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
package tests_helpers

/*
#include <openssl/ssl.h>
int X_SSL_get_security_level()
{
	int ret = -1;
	SSL_CTX *ctx = SSL_CTX_new(TLS_method());
	SSL *ssl = SSL_new(ctx);
	if(ssl == NULL)
        return ret;
	ret = SSL_get_security_level(ssl);
	if(ssl != NULL)
        SSL_free(ssl);
	if(ctx != NULL)
        SSL_CTX_free(ctx);
	return ret;
}
#cgo LDFLAGS: -lssl
*/
import "C"

var OpenSSLSecurityLevel = C.X_SSL_get_security_level()
