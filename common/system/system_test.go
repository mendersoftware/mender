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

package system

import (
	"bytes"
	"io"
	"strings"
	"testing"

	log "github.com/sirupsen/logrus"
	logtest "github.com/sirupsen/logrus/hooks/test"

	"github.com/stretchr/testify/assert"
)

func TestCommandLogger(t *testing.T) {
	var hook = logtest.NewGlobal()
	defer hook.Reset()
	log.SetLevel(log.DebugLevel)

	tests := map[string]struct {
		input    []string
		expected []string
	}{
		"single line": {
			input:    []string{"only one line"},
			expected: []string{"only one line"},
		},
		"verify stripping of newlines in text": {
			input:    []string{"foobar\n", "baz\n", "leftover"},
			expected: []string{"foobar", "baz", "leftover"},
		},
		"line with \r escape is logged verbatim": {
			input:    []string{"some line\ranother line", "next"},
			expected: []string{"some line\ranother line", "next"},
		},
		"multiline string is split in the logs": {
			input:    []string{"some line\nfoo", "next"},
			expected: []string{"some line", "foo", "next"},
		},
	}

	for name, test := range tests {
		t.Log(name)
		lgr := NewCmdLoggerStdout("foobar")
		buffer := bytes.NewBuffer(nil)
		for _, l := range test.input {
			buffer.WriteString(l)
		}
		_, err := io.Copy(lgr, buffer)
		assert.NoError(t, err)
		lgr.Flush()
		for _, l := range test.expected {
			assert.True(t, testLogContainsMessage(hook.AllEntries(), l))
		}
	}
}

func testLogContainsMessage(entries []*log.Entry, msg string) bool {
	for _, entry := range entries {
		if strings.Contains(entry.Message, msg) {
			return true
		}
	}
	return false
}
