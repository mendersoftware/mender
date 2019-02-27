package option

import (
	"errors"
	"net/http"
	"time"

	"github.com/itchio/httpkit/timeout"
	"github.com/itchio/wharf/state"
)

type EOSSettings struct {
	HTTPClient     *http.Client
	Consumer       *state.Consumer
	MaxTries       int
	ForceHTFSCheck bool
	HTFSDumpStats  bool
}

var defaultConsumer *state.Consumer

func init() {
	defaultHTTPClient = timeout.NewClient(time.Second*time.Duration(20), time.Second*time.Duration(10))
	setupHTTPClient(defaultHTTPClient)
}

func SetDefaultConsumer(consumer *state.Consumer) {
	defaultConsumer = consumer
}

var defaultHTTPClient *http.Client

func SetDefaultHTTPClient(c *http.Client) {
	setupHTTPClient(c)
	defaultHTTPClient = c
}

func DefaultSettings() *EOSSettings {
	return &EOSSettings{
		HTTPClient: defaultHTTPClient,
		Consumer:   defaultConsumer,
		MaxTries:   2,
	}
}

func setupHTTPClient(c *http.Client) {
	c.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= 10 {
			return errors.New("stopped after 10 redirects")
		}

		// see https://github.com/itchio/itch/issues/965
		// GitHub downloads redirect to AWS (at the time of this writing)
		// and golang's default redirect handler does not forward
		// headers like 'Range', which AS IT TURNS OUT are very important to us.
		ireq := via[0]
		for key, values := range ireq.Header {
			for _, value := range values {
				req.Header.Set(key, value)
			}
		}

		// see https://github.com/itchio/itch/issues/1960
		// if SourceForge sees a Referer (any Referer), it'll
		// serve us HTML instead of the actual download.
		// except, we're not a browser (don't build a browser on top of eos, please),
		// so we don't want HTML, ever.
		req.Header.Del("Referer")

		return nil
	}
}

//////////////////////////////////////

type Option interface {
	Apply(*EOSSettings)
}

//

type httpClientOption struct {
	client *http.Client
}

func (o *httpClientOption) Apply(settings *EOSSettings) {
	settings.HTTPClient = o.client
}

func WithHTTPClient(client *http.Client) Option {
	return &httpClientOption{client}
}

//

type consumerOption struct {
	consumer *state.Consumer
}

func (o *consumerOption) Apply(settings *EOSSettings) {
	settings.Consumer = o.consumer
}
func WithConsumer(consumer *state.Consumer) Option {
	return &consumerOption{consumer}
}

//

type maxTriesOption struct {
	maxTries int
}

func (o *maxTriesOption) Apply(settings *EOSSettings) {
	settings.MaxTries = o.maxTries
}

func WithMaxTries(maxTries int) Option {
	return &maxTriesOption{maxTries}
}

//

type htfsCheckOption struct{}

func (o *htfsCheckOption) Apply(settings *EOSSettings) {
	settings.ForceHTFSCheck = true
}

func WithHTFSCheck() Option {
	return &htfsCheckOption{}
}

//

type htfsDumpStatsOption struct{}

func (o *htfsDumpStatsOption) Apply(settings *EOSSettings) {
	settings.HTFSDumpStats = true
}

func WithHTFSDumpStats() Option {
	return &htfsDumpStatsOption{}
}
