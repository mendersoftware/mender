// Copyright 2019 Northern.tech AS
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

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
)

type MyWriter struct {
	writtenChunkSizes []int
	writtenData       bytes.Buffer
}

func (mw *MyWriter) Write(data []byte) (n int, err error) {
	mw.writtenChunkSizes = append(mw.writtenChunkSizes, len(data))
	n, err = mw.writtenData.Write(data)
	return
}

func (mw *MyWriter) GetTotalOutput() []byte {
	return mw.writtenData.Bytes()

}

func TestChunkedCopy(t *testing.T) {

	CHUNK_SIZE := int64(337)

	bldr := strings.Builder{}

	for i := 0; i < 1000; i++ {
		bldr.WriteString(fmt.Sprintf("x%07d", i)) // can ignore return value
	}

	input_bytes := []byte(bldr.String())

	mw := &MyWriter{}

	bytes_written, err := chunkedCopy(mw, bytes.NewBuffer(input_bytes), CHUNK_SIZE) // use a strange chunk size just to prove it works

	// a Copy() routine should return err == nil on EOF.
	if err != nil {
		t.Errorf("chunkedCopy: expected err to be nil but got: %v", err)
	}

	if int(bytes_written) != len(input_bytes) {
		t.Errorf("chunkedCopy: did not copy full input: expected %v, got %v", len(input_bytes), bytes_written)
	}

	output_bytes := mw.GetTotalOutput()

	if !bytes.Equal(output_bytes, input_bytes) {
		t.Errorf("chunkedCopy: output did not match input")
	}

	total_of_chunks := 0

	// This is what we really want to check. All of the calls to Write (except the last) must be of size CHUNK_SIZE
	for i := 0; i < len(mw.writtenChunkSizes)-1; i++ {
		if mw.writtenChunkSizes[i] != int(CHUNK_SIZE) {
			t.Errorf("chunkedCopy: writtenChunkSizes[%d] = %v, expected %v", i, mw.writtenChunkSizes[i], CHUNK_SIZE)
		}
		total_of_chunks += mw.writtenChunkSizes[i]
	}
	total_of_chunks += mw.writtenChunkSizes[len(mw.writtenChunkSizes)-1] // include last chunk in sum

	if total_of_chunks != len(input_bytes) {
		t.Errorf("chunkedCopy: sum of writtenChunkSizes (%v) does not match expected size (%v)",
			total_of_chunks,
			len(input_bytes),
		)
	}

}
