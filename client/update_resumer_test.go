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

package client

import (
	"fmt"
	"github.com/stretchr/testify/assert"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
)

type testHandler struct {
	t    *testing.T
	addr string

	brokenContentLength     bool
	missingContentLength    bool
	earlyRangeStart         bool
	lateRangeStart          bool
	noPartialContentSupport bool
	customContentRange      string
	missingContentRange     bool
	garbledContentStart     bool
	breakAfterShortRange    bool
	serverDownAfter         time.Duration
	serverUpAgainAfter      time.Duration

	success bool
}

func (h *testHandler) ServeHTTP(res http.ResponseWriter, req *http.Request) {
	t := h.t

	hRangeStr := req.Header.Get("Range")
	var code int
	var pos int64
	var err error

	f, err := os.Open("update_resumer_test.go")
	assert.NoError(t, err)
	stat, err := f.Stat()
	assert.NoError(t, err)
	size := stat.Size()

	if len(hRangeStr) > 0 && !h.noPartialContentSupport {
		code = http.StatusPartialContent
		assert.True(t, strings.HasPrefix(hRangeStr, "bytes="))
		hRange := strings.Split(hRangeStr[len("bytes="):], "-")
		assert.Equal(t, 2, len(hRange))
		pos, err = strconv.ParseInt(hRange[0], 10, 64)
		assert.NoError(t, err)
		if h.earlyRangeStart {
			pos -= 5
		} else if h.lateRangeStart {
			pos += 5
		}
		if h.missingContentRange {
			res.Header().Set("Content-Range", "")
		} else if h.customContentRange != "" {
			res.Header().Set("Content-Range", h.customContentRange)
		} else if h.missingContentLength {
			res.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d", pos, size-1))
		} else if h.garbledContentStart {
			res.Header().Set("Content-Range", fmt.Sprintf("bytes abc-%d/%d", size-1, size))
		} else {
			if h.brokenContentLength {
				size -= 1
			}
			res.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", pos, size-1, size))
		}
	} else {
		code = http.StatusOK
		pos = 0
	}

	res.Header().Set("Content-Length", fmt.Sprintf("%d", size-pos))

	_, err = f.Seek(pos, os.SEEK_SET)
	assert.NoError(t, err)

	res.WriteHeader(code)
	// Only give some, not all, then terminate connection.
	toCopy := size / 5
	if h.breakAfterShortRange && len(hRangeStr) > 0 {
		// Terminate before we even get to the part the client is
		// interested in.
		toCopy = 2
		// Only do this once.
		h.breakAfterShortRange = false
	}
	if toCopy > size-pos {
		toCopy = size - pos
	}
	_, err = io.CopyN(res, f, toCopy)

	if h.success {
		assert.NoError(t, err)
	}
}

func testBrokenReadAndPartialDownload_oneCase(t *testing.T, h *testHandler) {
	t.Parallel()

	var server http.Server
	server.Addr = h.addr

	server.Handler = h

	f, err := os.Open("update_resumer_test.go")
	assert.NoError(t, err)
	expected, err := ioutil.ReadAll(f)
	assert.NoError(t, err)

	server.SetKeepAlivesEnabled(false)

	go server.ListenAndServe()
	defer server.Close()

	var client http.Client
	portAttempts := 5
	for {
		_, err := client.Get(fmt.Sprintf("http://localhost%s/", h.addr))
		// Wait until port is open
		if err == nil {
			break
		}
		time.Sleep(time.Second)
		portAttempts -= 1
		if portAttempts <= 0 {
			t.Fatalf("Port %s never opened!", server.Addr)
		}
	}

	req, err := http.NewRequest("GET", fmt.Sprintf("http://localhost%s/update_resumer_test.go", h.addr), nil)
	assert.NoError(t, err)
	res, err := client.Do(req)
	assert.NoError(t, err)

	contentLength, err := strconv.ParseInt(res.Header.Get("Content-Length"), 10, 64)
	assert.NoError(t, err)

	updateResumer := NewUpdateResumer(res.Body, contentLength, 3*time.Second, &client, req)
	defer updateResumer.Close()

	if h.serverDownAfter > 0 {
		go func() {
			time.Sleep(h.serverDownAfter)
			server.Close()
			if h.serverUpAgainAfter > 0 {
				time.Sleep(h.serverUpAgainAfter)
				server.ListenAndServe()
			}
		}()
	}

	actual, err := ioutil.ReadAll(updateResumer)
	if h.success {
		assert.NoError(t, err)
		assert.Equal(t, string(expected), string(actual))
	} else {
		// Everything read up until the error should be correct.
		assert.Equal(t, string(expected[:len(actual)]), string(actual))
		assert.Error(t, err)
	}
}

func testBrokenReadAndPartialDownload_group(t *testing.T) {
	var base testHandler
	base.t = t

	{
		h := base
		h.addr = ":9753"
		h.success = true
		t.Run("success", func(t *testing.T) {
			testBrokenReadAndPartialDownload_oneCase(t, &h)
		})
	}

	{
		h := base
		h.addr = ":9754"
		h.success = true
		h.earlyRangeStart = true
		t.Run("earlyRangeStart", func(t *testing.T) {
			testBrokenReadAndPartialDownload_oneCase(t, &h)
		})
	}

	{
		h := base
		h.addr = ":9755"
		h.success = false
		h.lateRangeStart = true
		t.Run("lateRangeStart", func(t *testing.T) {
			testBrokenReadAndPartialDownload_oneCase(t, &h)
		})
	}

	{
		h := base
		h.addr = ":9756"
		h.success = false
		h.brokenContentLength = true
		t.Run("brokenContentLength", func(t *testing.T) {
			testBrokenReadAndPartialDownload_oneCase(t, &h)
		})
	}

	{
		h := base
		h.addr = ":9757"
		h.success = true
		h.missingContentLength = true
		t.Run("missingContentLength", func(t *testing.T) {
			testBrokenReadAndPartialDownload_oneCase(t, &h)
		})
	}

	{
		h := base
		h.addr = ":9758"
		h.success = false
		h.noPartialContentSupport = true
		t.Run("noPartialContentSupport", func(t *testing.T) {
			testBrokenReadAndPartialDownload_oneCase(t, &h)
		})
	}

	{
		h := base
		h.addr = ":9759"
		h.success = false
		h.customContentRange = "bytes "
		t.Run("emptyContentRange", func(t *testing.T) {
			testBrokenReadAndPartialDownload_oneCase(t, &h)
		})
	}

	{
		h := base
		h.addr = ":9760"
		h.success = false
		h.customContentRange = "bytes abc-def/deadbeef"
		t.Run("formattedButInvalidContentRange", func(t *testing.T) {
			testBrokenReadAndPartialDownload_oneCase(t, &h)
		})
	}

	{
		h := base
		h.addr = ":9761"
		h.success = false
		h.customContentRange = "bytes 5"
		t.Run("improperlyFormattedContentRange", func(t *testing.T) {
			testBrokenReadAndPartialDownload_oneCase(t, &h)
		})
	}

	{
		h := base
		h.addr = ":9762"
		h.success = false
		h.customContentRange = "5-6/2"
		t.Run("missingBytesContentRange", func(t *testing.T) {
			testBrokenReadAndPartialDownload_oneCase(t, &h)
		})
	}

	{
		h := base
		h.addr = ":9763"
		h.success = false
		h.missingContentRange = true
		t.Run("missingContentRange", func(t *testing.T) {
			testBrokenReadAndPartialDownload_oneCase(t, &h)
		})
	}

	{
		h := base
		h.addr = ":9764"
		h.success = false
		h.customContentRange = "bytes 5-6/20 7-8/20"
		t.Run("tooManyContentRanges", func(t *testing.T) {
			testBrokenReadAndPartialDownload_oneCase(t, &h)
		})
	}

	{
		h := base
		h.addr = ":9765"
		h.success = false
		h.garbledContentStart = true
		t.Run("garbledContentStart", func(t *testing.T) {
			testBrokenReadAndPartialDownload_oneCase(t, &h)
		})
	}

	{
		h := base
		h.addr = ":9766"
		h.success = true
		h.earlyRangeStart = true
		h.breakAfterShortRange = true
		t.Run("breakAfterShortRange", func(t *testing.T) {
			testBrokenReadAndPartialDownload_oneCase(t, &h)
		})
	}

	{
		h := base
		h.addr = ":9767"
		h.success = true
		h.serverDownAfter = 3 * time.Second
		h.serverUpAgainAfter = 5 * time.Second
		t.Run("serverDownAndUp", func(t *testing.T) {
			testBrokenReadAndPartialDownload_oneCase(t, &h)
		})
	}

	{
		h := base
		h.addr = ":9768"
		h.success = false
		h.serverDownAfter = 3 * time.Second
		t.Run("serverDown", func(t *testing.T) {
			testBrokenReadAndPartialDownload_oneCase(t, &h)
		})
	}
}

func TestBrokenReadAndPartialDownload(t *testing.T) {
	oldExponentialBackoffSmallestUnit := exponentialBackoffSmallestUnit
	// Set this to a second to make tests go faster.
	exponentialBackoffSmallestUnit = time.Second
	defer func() {
		exponentialBackoffSmallestUnit = oldExponentialBackoffSmallestUnit
	}()

	t.Run("group", testBrokenReadAndPartialDownload_group)
}
