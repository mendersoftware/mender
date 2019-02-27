package netx

import (
	"io"
	"net"
	"sync/atomic"
	"time"

	"github.com/getlantern/errors"
)

var (
	copyTimeout = 1 * time.Second
)

// BidiCopy copies between in and out in both directions using the specified
// buffers, returning the errors from copying to out and copying to in.
func BidiCopy(out net.Conn, in net.Conn, bufOut []byte, bufIn []byte) (outErr error, inErr error) {
	stop := uint32(0)
	outErrCh := make(chan error, 1)
	inErrCh := make(chan error, 1)
	go doCopy(out, in, bufIn, outErrCh, &stop)
	go doCopy(in, out, bufOut, inErrCh, &stop)
	return <-outErrCh, <-inErrCh
}

// doCopy is based on io.copyBuffer
func doCopy(dst net.Conn, src net.Conn, buf []byte, errCh chan error, stop *uint32) {
	var err error
	defer func() {
		atomic.StoreUint32(stop, 1)
		dst.SetReadDeadline(time.Now().Add(copyTimeout))
		errCh <- err
	}()

	defer func() {
		p := recover()
		if p != nil {
			err = errors.New("Panic while copying: %v", p)
		}
	}()

	for {
		stopping := atomic.LoadUint32(stop) == 1
		if stopping {
			src.SetReadDeadline(time.Now().Add(copyTimeout))
		}
		nr, er := src.Read(buf)
		if nr > 0 {
			nw, ew := dst.Write(buf[0:nr])
			if ew != nil {
				err = ew
				return
			}
			if nw != nr {
				err = io.ErrShortWrite
				return
			}
		}
		if er == io.EOF {
			return
		}
		if er != nil {
			if IsTimeout(er) {
				if stopping {
					return
				}
			} else {
				err = er
				return
			}
		}
	}
}

// IsTimeout indicates whether the given error is a network timeout error
func IsTimeout(err error) bool {
	if nerr, ok := err.(net.Error); ok && nerr.Timeout() {
		return true
	}
	return false
}
