package idletiming

import (
	"io"
	"net"
	"time"
)

// NewReader creates a Reader whose Read method only blocks up to timeout. If
// timeout is hit, it returns whatever was read and no error.
func NewReader(conn net.Conn, timeout time.Duration) io.Reader {
	return &reader{
		Conn:    conn,
		timeout: timeout,
	}
}

type reader struct {
	net.Conn
	timeout time.Duration
}

func (r *reader) Read(b []byte) (int, error) {
	r.SetReadDeadline(time.Now().Add(r.timeout))
	n, err := r.Conn.Read(b)
	if isTimeout(err) {
		err = nil
	}
	return n, err
}
