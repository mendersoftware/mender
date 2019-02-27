package htfs

import "fmt"

type needsRenewalError struct {
	url string
}

func (nre *needsRenewalError) Error() string {
	return "url has expired and needs renewal"
}

// ServerErrorCode represents an error condition where
// some server does not support htfs - perhaps because
// it has no range support, or because it returned a bad HTTP status code.
type ServerErrorCode int64

const (
	// ServerErrorCodeUnknown does not map to any known errors.
	// It's used for any unexpected HTTP status codes.
	ServerErrorCodeUnknown ServerErrorCode = iota
	// ServerErrorCodeNoRangeSupport indicates that the remote
	// server does not support HTTP Range Requests:
	// https://developer.mozilla.org/en-US/docs/Web/HTTP/Range_requests
	ServerErrorCodeNoRangeSupport
)

// ServerError represents an error htfs has encountered
// when talking to a remote server.
type ServerError struct {
	Host       string
	Message    string
	Code       ServerErrorCode
	StatusCode int
}

func (se *ServerError) Error() string {
	return fmt.Sprintf("%s: %s", se.Host, se.Message)
}
