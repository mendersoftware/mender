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
package utils

import (
	"bufio"
	"io"
	"strings"

	"github.com/pkg/errors"
)

// KeyValParser is a parser that reading lines in format 'key=value\n'. Entries
// collected during parsing and can be retrieved by calling
// KeyValParser.Collect(). Keys appearing multiple times will have their values
// merged into a single list.
type KeyValParser struct {
	data map[string][]string
}

func (k *KeyValParser) Parse(raw io.Reader) error {
	if k.data == nil {
		k.data = map[string][]string{}
	}

	in := bufio.NewScanner(raw)

	for in.Scan() {
		if err := in.Err(); err != nil {
			return errors.Wrapf(err, "failed to read input line")
		}
		line := in.Text()

		if len(line) == 0 {
			continue
		}

		val := strings.SplitN(line, "=", 2)

		if len(val) < 2 {
			return errors.Errorf("incorrect line '%s'", line)
		}

		if _, ok := k.data[val[0]]; ok {
			k.data[val[0]] = append(k.data[val[0]], val[1])
		} else {
			k.data[val[0]] = []string{val[1]}
		}
	}
	return nil
}

// Collect() data read during Parse(). Map keys correspond to entry names, while
// map values is a list of entry values collected for particular key.
//
// For instance, input:
//
//    foo=bar
//    baz=1
//    baz=zen
//
// will be converted to:
//
//    map[string][]string{
//        "foo": []string{"bar"},
//        "baz": []string{"1", "zen"}
//    }
//
// If no data was collected during Parse(), returns nil. A non-nil may be
// returned regardless of errors reported byParse(), in such case, the data will
// contain entries collected up to a point when error was detected.
func (k *KeyValParser) Collect() map[string][]string {
	return k.data
}
