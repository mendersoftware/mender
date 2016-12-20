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
package utils

import (
	"bytes"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
)

func writeZeros(out io.Writer, cnt int64) {
	for i := int64(0); i < cnt; i++ {
		out.Write([]byte{0})
	}
}

func TestProgress(t *testing.T) {
	b := &bytes.Buffer{}
	p := &ProgressWriter{
		Out: b,
		N:   100,
	}

	n, err := p.Write([]byte{})
	assert.NoError(t, err)
	assert.Equal(t, 0, n)

	writeZeros(p, 100)
	assert.Equal(t, ".                                100% 100 B\n", b.String())

	b = &bytes.Buffer{}
	p = &ProgressWriter{
		Out: b,
		N:   1024,
	}
	writeZeros(p, 1024)
	assert.Equal(t, ".                                100% 1 KiB\n", b.String())

	b = &bytes.Buffer{}
	p = &ProgressWriter{
		Out: b,
		N:   1024 * 800,
	}
	// 800KiB
	writeZeros(p, 1024*800)
	assert.Equal(t, ".........................        100% 800 KiB\n", b.String())

	b = &bytes.Buffer{}
	p = &ProgressWriter{
		Out: b,
		N:   4 * 1024 * 1024,
	}
	// 4MiB
	writeZeros(p, 4*1024*1024)
	assert.Equal(t,
		`................................  25% 1024 KiB
................................  50% 2048 KiB
................................  75% 3072 KiB
................................ 100% 4096 KiB
`,
		b.String())

	// do not specify the maximum size, % progress will not be displayed,
	// but KiB will
	b = &bytes.Buffer{}
	p = &ProgressWriter{
		Out: b,
	}
	// 4MiB
	writeZeros(p, 4*1024*1024)
	assert.Equal(t,
		`................................ 1024 KiB
................................ 2048 KiB
................................ 3072 KiB
................................ 4096 KiB
`,
		b.String())

	// same thing again, but now write less than full lines, a special case,
	// since we don't know the full size, we cannot tell when the data
	// stream will finish, so newlines and progress report only appears
	// after a full line is complete
	b = &bytes.Buffer{}
	p = &ProgressWriter{
		Out: b,
	}
	// 3.5MiB
	writeZeros(p, 3*1024*1024+512*1024)
	assert.Equal(t,
		`................................ 1024 KiB
................................ 2048 KiB
................................ 3072 KiB
................`,
		b.String())

	// check if a warning is written if we go over declared size
	b = &bytes.Buffer{}
	p = &ProgressWriter{
		Out: b,
		N:   100,
	}
	// 1.5MiB
	writeZeros(p, 1*1024*1024+512*1024)
	assert.Equal(t,
		`.                                100% 100 B
going over declared size, expected 100 N, now 101
................................ 1024 KiB
................`,
		b.String())

}
