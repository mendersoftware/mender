package htfs

import (
	"bufio"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/url"
	"time"

	"github.com/itchio/httpkit/htfs/backtracker"
	"github.com/pkg/errors"
)

type conn struct {
	backtracker.Backtracker

	file       *File
	id         string
	touchedAt  time.Time
	body       io.ReadCloser
	reader     *bufio.Reader
	currentURL string

	header        http.Header
	requestURL    *url.URL
	statusCode    int
	contentLength int64
}

func (c *conn) Stale() bool {
	return time.Since(c.touchedAt) > c.file.ConnStaleThreshold
}

// *not* thread-safe, File handles the locking
func (c *conn) Connect(offset int64) error {
	hf := c.file

	if c.body != nil {
		err := c.body.Close()
		if err != nil {
			return err
		}

		c.body = nil
		c.reader = nil
	}

	retryCtx := hf.newRetryContext()
	renewalTries := 0

	hf.currentURL = hf.getCurrentURL()
	for retryCtx.ShouldTry() {
		startTime := time.Now()
		err := c.tryConnect(offset)
		if err != nil {
			if _, ok := err.(*needsRenewalError); ok {
				renewalTries++
				if renewalTries >= maxRenewals {
					return ErrTooManyRenewals
				}
				hf.log("[%9d-%9d] (Connect) renewing on %v", offset, offset, err)

				err = c.renewURLWithRetries(offset)
				if err != nil {
					// if we reach this point, we've failed to generate
					// a download URL a bunch of times in a row
					return err
				}
				continue
			} else if hf.shouldRetry(err) {
				hf.log("[%9d-%9d] (Connect) retrying %v", offset, offset, err)
				retryCtx.Retry(err)
				continue
			} else {
				return err
			}
		}

		totalConnDuration := time.Since(startTime)
		hf.log("[%9d-%9d] (Connect) %s", offset, offset, totalConnDuration)
		hf.stats.connections++
		hf.stats.connectionWait += totalConnDuration
		return nil
	}

	return errors.WithMessage(retryCtx.LastError, "htfs connect")
}

func (c *conn) renewURLWithRetries(offset int64) error {
	hf := c.file
	renewRetryCtx := hf.newRetryContext()

	for renewRetryCtx.ShouldTry() {
		var err error
		hf.stats.renews++
		c.currentURL, err = hf.renewURL()
		if err != nil {
			if hf.shouldRetry(err) {
				hf.log("[%9d-%9d] (Connect) retrying %v", offset, offset, err)
				renewRetryCtx.Retry(err)
				continue
			} else {
				hf.log("[%9d-%9d] (Connect) bailing on %v", offset, offset, err)
				return err
			}
		}

		return nil
	}
	return errors.WithMessage(renewRetryCtx.LastError, "htfs renew")
}

func (c *conn) tryConnect(offset int64) error {
	hf := c.file

	req, err := http.NewRequest("GET", hf.currentURL, nil)
	if err != nil {
		return err
	}

	byteRange := fmt.Sprintf("bytes=%d-", offset)
	req.Header.Set("Range", byteRange)

	res, err := hf.client.Do(req)
	if err != nil {
		return err
	}

	if res.StatusCode == 200 && offset > 0 {
		defer res.Body.Close()
		return errors.WithStack(&ServerError{Host: req.Host, Message: fmt.Sprintf("HTTP Range header not supported"), Code: ServerErrorCodeNoRangeSupport, StatusCode: res.StatusCode})
	}

	if res.StatusCode/100 != 2 {
		defer res.Body.Close()

		body, err := ioutil.ReadAll(res.Body)
		if err != nil {
			body = []byte("could not read error body")
		}

		if hf.needsRenewal(res, body) {
			return &needsRenewalError{url: hf.currentURL}
		}

		return errors.WithStack(&ServerError{Host: req.Host, Message: fmt.Sprintf("HTTP %d: %v", res.StatusCode, string(body)), StatusCode: res.StatusCode})
	}

	c.Backtracker = backtracker.New(offset, res.Body, maxDiscard)
	c.body = res.Body
	c.header = res.Header
	c.requestURL = res.Request.URL
	c.statusCode = res.StatusCode
	c.contentLength = res.ContentLength

	return nil
}

func (c *conn) Close() error {
	if c.body != nil {
		err := c.body.Close()
		c.body = nil

		if err != nil {
			return err
		}
	}

	return nil
}
