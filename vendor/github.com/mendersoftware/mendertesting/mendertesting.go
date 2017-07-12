// Copyright 2017 Northern.tech AS
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

package mendertesting

import "fmt"
import "runtime"
import "strings"
import "testing"

func failWithPrefixf(t *testing.T, stackDepth int, msg string, args ...interface{}) {
	_, file, line, ok := runtime.Caller(stackDepth + 1)
	var prefix string
	if ok {
		prefix = fmt.Sprintf("%s:%d", file, line)
	} else {
		prefix = "<unknown>:FAIL"
	}
	var newArgs []interface{}
	newArgs = append(newArgs, prefix)
	newArgs = append(newArgs, args...)
	prefixed_msg := fmt.Sprintf("%s: "+msg, newArgs...)
	t.Fatal(prefixed_msg)
}

// Asserts that condition is true.
func AssertTrue(t *testing.T, cond bool) {
	if !cond {
		failWithPrefixf(t, 1, "FAIL")
	}
}

// Asserts that the two strings are identical.
func AssertStringEqual(t *testing.T, str1 string, str2 string) {
	if str1 != str2 {
		failWithPrefixf(t, 1, "\"%s\" != \"%s\"", str1, str2)
	}
}

// Asserts that the string occurs somewhere in the error message.
func AssertErrorSubstring(t *testing.T, err error, sub string) {
	if strings.Index(err.Error(), sub) < 0 {
		failWithPrefixf(t, 1, "'%s' does not occur in error '%s'",
			sub, err.Error())
	}
}

func AssertNoError(t *testing.T, err error) {
	if err != nil {
		failWithPrefixf(t, 1, "Error not expected: %s", err.Error())
	}
}
