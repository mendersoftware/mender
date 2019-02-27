// Package timeout provides an http.Client that closes a connection if it takes
// too long to establish, or stays idle for too long.
package timeout

import (
	"crypto/tls"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/efarrer/iothrottler"
	"github.com/getlantern/idletiming"
	"github.com/pkg/errors"
	"golang.org/x/net/http2"
)

// IgnoreCertificateErrors is a dangerous option that instructs all
// timeout clients to ignore HTTPS certificate errors. This is used
// mostly in development, to use debugging proxies together with any
// timeout-powered application.
var IgnoreCertificateErrors = os.Getenv("HTTPKIT_IGNORE_CERTIFICATE_ERRORS") == "1"

const (
	// DefaultConnectTimeout is the default duration we're willing to wait to establish a connection
	DefaultConnectTimeout time.Duration = 30 * time.Second
	// DefaultIdleTimeout is the duration after which, if there's no I/O activity, we declare a connection dead
	DefaultIdleTimeout = 60 * time.Second
)

// ThrottlerPool is the singleton pool from `iothrottler`
// that package timeout uses to manage all connections.
var ThrottlerPool *iothrottler.IOThrottlerPool

func init() {
	ThrottlerPool = iothrottler.NewIOThrottlerPool(iothrottler.Unlimited)
}

var simulateOffline = false
var conns = make(map[net.Conn]bool)

// SetSimulateOffline enables or disables offline simulation.
// When enabled, all connection attempts will return a *net.OpError
// with "simulated offline" contained in its string representation.
func SetSimulateOffline(enabled bool) {
	simulateOffline = enabled
}

func timeoutDialer(cTimeout time.Duration, rwTimeout time.Duration) func(net, addr string) (net.Conn, error) {
	return func(netw, addr string) (net.Conn, error) {
		if simulateOffline {
			return nil, &net.OpError{
				Op:  "dial",
				Err: errors.New("simulated offline"),
			}
		}

		// if it takes too long to establish a connection, give up
		timeoutConn, err := net.DialTimeout(netw, addr, cTimeout)
		if err != nil {
			return nil, errors.WithStack(err)
		}
		// respect global throttle settings
		throttledConn, err := ThrottlerPool.AddConn(timeoutConn)
		if err != nil {
			return nil, errors.WithStack(err)
		}
		// measure bps
		monitorConn := &monitoringConn{
			Conn: throttledConn,
		}
		// if we stay idle too long, close
		idleConn := idletiming.Conn(monitorConn, rwTimeout, func() {
			monitorConn.Close()
		})
		return idleConn, nil
	}
}

// NewClient returns a new http client with custom connect and r/w timeouts.
func NewClient(connectTimeout time.Duration, readWriteTimeout time.Duration) *http.Client {
	transport := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		Dial:  timeoutDialer(connectTimeout, readWriteTimeout),
	}
	if IgnoreCertificateErrors {
		transport.TLSClientConfig = &tls.Config{
			InsecureSkipVerify: true,
		}
	}
	http2.ConfigureTransport(transport)
	return &http.Client{
		Transport: transport,
	}
}

// NewDefaultClient returns a new http client with default connect and r/w timeouts.
func NewDefaultClient() *http.Client {
	return NewClient(DefaultConnectTimeout, DefaultIdleTimeout)
}
