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

package client

import (
	"fmt"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	"io"
	"io/ioutil"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type UpdateResumer struct {
	stream        io.ReadCloser
	apiReq        ApiRequester
	req           *http.Request
	offset        int64
	contentLength int64
	retryAttempts int
	maxWait       time.Duration
}

// Note: It is important that nothing has been read from the stream yet.
func NewUpdateResumer(stream io.ReadCloser,
	contentLength int64,
	maxWait time.Duration,
	apiReq ApiRequester,
	req *http.Request) *UpdateResumer {

	return &UpdateResumer{
		stream:        stream,
		apiReq:        apiReq,
		req:           req,
		contentLength: contentLength,
		maxWait:       maxWait,
	}
}

func (h *UpdateResumer) Read(buf []byte) (int, error) {
	origOffset := h.offset
	for {
		bytesRead, err := h.stream.Read(buf[h.offset-origOffset:])
		if bytesRead > 0 {
			h.offset += int64(bytesRead)
		}
		if err == nil ||
			h.offset <= 0 ||
			(err == io.EOF && h.offset >= h.contentLength) {

			return int(h.offset - origOffset), err
		}

		// If we get here we have unexpected EOF, either an actual unexpected
		// EOF, or a normal EOF, but with an unexpected number of bytes. This is
		// a sign that we should try to resume from the same position.

		h.req.Header.Set("Range", fmt.Sprintf("bytes=%d-", h.offset))

		var res *http.Response
		for {
			log.Errorf("Download connection broken: %s", err.Error())

			waitTime, err := GetExponentialBackoffTime(h.retryAttempts, h.maxWait)
			if err != nil {
				return int(h.offset - origOffset),
					errors.Wrapf(err, "Cannot resume download")
			}

			log.Infof("Resuming download in %s", waitTime.String())
			h.retryAttempts += 1

			time.Sleep(waitTime)

			log.Infof("Attempting to resume artifact download from offset %d", h.offset)

			res, err = h.apiReq.Do(h.req)
			if err != nil {
				log.Infof("Download resume request failed: %s", err.Error())
				continue
			}

			stream, err := h.getStreamFromPartialContent(res)
			if err != nil {
				continue
			}

			h.stream = stream
			break
		}

		// Repeat from the top.
	}
}

func (h *UpdateResumer) getStreamFromPartialContent(res *http.Response) (io.ReadCloser, error) {
	var err error

	if h.offset > 0 && res.StatusCode != http.StatusPartialContent {
		return nil, fmt.Errorf("Could not resume download from offset %d. HTTP status code: %s",
			h.offset, res.Status)
	}

	hRangeStr := res.Header.Get("Content-Range")
	log.Debugf("Content-Range received from server: '%s'", hRangeStr)
	if !strings.HasPrefix(hRangeStr, "bytes ") {
		return nil, fmt.Errorf("HTTP server returned garbled or missing range: '%s'", hRangeStr)
	}
	hRangeStr = strings.TrimSpace(hRangeStr[len("bytes "):])

	hRangePosAndSize := strings.Split(hRangeStr, "/")
	if len(hRangePosAndSize) > 2 {
		return nil, fmt.Errorf("Unexpected Content-Range received from server: %s", hRangeStr)
	} else if len(hRangePosAndSize) == 2 {
		var sizeFromServer int64
		sizeFromServer, err = strconv.ParseInt(hRangePosAndSize[1], 10, 64)
		if err != nil {
			return nil, fmt.Errorf("HTTP server returned garbled or missing range: '%s'", hRangeStr)
		} else if sizeFromServer != h.contentLength {
			return nil, fmt.Errorf("Size of artifact changed after download was resumed "+
				"(expected %d, got %d)", h.contentLength, sizeFromServer)
		}
		// Intentional fallthrough. Response does not have to contain
		// the total size after '/'.
	}
	hRangeStartAndEnd := strings.Split(hRangePosAndSize[0], "-")
	if len(hRangeStartAndEnd) != 2 {
		return nil, fmt.Errorf("Invalid Content-Range returned by server: '%s'", hRangeStr)
	}

	var newOffset int64
	newOffset, err = strconv.ParseInt(hRangeStartAndEnd[0], 10, 64)
	if err != nil {
		return nil, errors.Wrapf(err, "HTTP server returned garbled range: %s", hRangeStr)
	}

	if newOffset > h.offset {
		return nil, fmt.Errorf("HTTP server did not return expected range. Expected %d, got %d",
			h.offset, newOffset)
	} else if newOffset < h.offset {
		// Server gave us an offset which is earlier than we asked.
		// Consume input to get back where we were.
		bytesRead, err := io.CopyN(ioutil.Discard, res.Body, h.offset-newOffset)
		if err == io.ErrUnexpectedEOF {
			// Treat this specifically to force a retry in the outer function.
			return nil, err
		} else if err != nil || bytesRead != h.offset-newOffset {
			return nil, errors.Wrapf(err,
				"Could not resume download, unable to catch up to offset %d from offset %d",
				h.offset, newOffset)
		}
		// Intentional fallthrough to end.
	}

	return res.Body, nil
}

func (h *UpdateResumer) Close() error {
	return h.stream.Close()
}
