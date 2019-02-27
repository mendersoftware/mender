// Package eos stands for 'enhanced os', it mostly supplies 'eos.Open', which supports
// the 'itchfs://' scheme to access remote files
package eos

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"

	"github.com/itchio/httpkit/htfs"
	"github.com/itchio/httpkit/retrycontext"
	"github.com/itchio/wharf/eos/option"
	"github.com/pkg/errors"
)

var htfsLogLevel = os.Getenv("HTFS_DEBUG")
var htfsCheck = os.Getenv("HTFS_CHECK") == "1"
var hfSeed = 0

type File interface {
	io.Reader
	io.Closer
	io.ReaderAt
	io.Seeker

	Stat() (os.FileInfo, error)
}

type Handler interface {
	Scheme() string
	MakeResource(u *url.URL) (htfs.GetURLFunc, htfs.NeedsRenewalFunc, error)
}

var handlers = make(map[string]Handler)

func RegisterHandler(h Handler) error {
	scheme := h.Scheme()

	if handlers[scheme] != nil {
		return fmt.Errorf("already have a handler for %s:", scheme)
	}

	handlers[h.Scheme()] = h
	return nil
}

func DeregisterHandler(h Handler) {
	delete(handlers, h.Scheme())
}

type simpleHTTPResource struct {
	url string
}

func (shr *simpleHTTPResource) GetURL() (string, error) {
	return shr.url, nil
}

func (shr *simpleHTTPResource) NeedsRenewal(res *http.Response, body []byte) bool {
	return false
}

func Open(name string, opts ...option.Option) (File, error) {
	f, err := realOpen(name, opts...)
	if err != nil {
		return nil, err
	}

	settings := option.DefaultSettings()
	for _, opt := range opts {
		opt.Apply(settings)
	}
	forceHtfsCheck := htfsCheck || settings.ForceHTFSCheck

	if hf, ok := f.(*htfs.File); ok && forceHtfsCheck {
		hf.ForbidBacktracking = true

		f2, err := realOpen(name, opts...)
		if err != nil {
			return nil, err
		}

		return &CheckingFile{
			Reference: f,
			Trainee:   f2,
		}, nil
	}

	return f, err
}

func realOpen(name string, opts ...option.Option) (File, error) {
	settings := option.DefaultSettings()

	for _, opt := range opts {
		opt.Apply(settings)
	}

	if name == "/dev/null" {
		return &emptyFile{}, nil
	}

	u, err := url.Parse(name)
	if err != nil {
		return nil, errors.WithStack(err)
	}

	htfsSettings := func() *htfs.Settings {
		s := &htfs.Settings{
			Client: settings.HTTPClient,
			RetrySettings: &retrycontext.Settings{
				MaxTries: settings.MaxTries,
				Consumer: settings.Consumer,
			},
			DumpStats: settings.HTFSDumpStats,
		}

		if htfsLogLevel != "" {
			hfSeed++
			hfIndex := hfSeed

			s.Log = func(msg string) {
				fmt.Fprintf(os.Stderr, "[hf%d] %s\n", hfIndex, msg)
			}
			numericLevel, err := strconv.ParseInt(htfsLogLevel, 10, 64)
			if err == nil {
				s.LogLevel = int(numericLevel)
			}
		}
		return s
	}

	switch u.Scheme {
	case "http", "https":
		res := &simpleHTTPResource{name}
		hf, err := htfs.Open(res.GetURL, res.NeedsRenewal, htfsSettings())

		if err != nil {
			return nil, err
		}

		return hf, nil
	default:
		handler := handlers[u.Scheme]
		if handler == nil {
			return os.Open(name)
		}

		getURL, needsRenewal, err := handler.MakeResource(u)
		if err != nil {
			return nil, errors.WithStack(err)
		}

		hf, err := htfs.Open(getURL, needsRenewal, htfsSettings())

		if err != nil {
			return nil, err
		}

		return hf, nil
	}
}

func Redact(name string) string {
	u, err := url.Parse(name)
	if err != nil {
		return name
	}

	return u.Path
}
