package timeout

import (
	"net"
	"sync"
	"time"
)

var lastBandwidthUpdate time.Time
var bytesSinceLastUpdate float64
var maxBucketDuration = 1 * time.Second
var bps float64
var lock sync.Mutex

type monitoringConn struct {
	Conn net.Conn
}

var _ net.Conn = (*monitoringConn)(nil)

func (mc *monitoringConn) Close() error {
	return mc.Conn.Close()
}

func (mc *monitoringConn) LocalAddr() net.Addr {
	return mc.Conn.LocalAddr()
}

func (mc *monitoringConn) RemoteAddr() net.Addr {
	return mc.Conn.RemoteAddr()
}

func (mc *monitoringConn) SetDeadline(t time.Time) error {
	return mc.Conn.SetDeadline(t)
}

func (mc *monitoringConn) SetReadDeadline(t time.Time) error {
	return mc.Conn.SetReadDeadline(t)
}

func (mc *monitoringConn) SetWriteDeadline(t time.Time) error {
	return mc.Conn.SetWriteDeadline(t)
}

func (mc *monitoringConn) Read(buf []byte) (int, error) {
	readBytes, err := mc.Conn.Read(buf)
	recordBytesRead(int64(readBytes))
	return readBytes, err
}

func (mc *monitoringConn) Write(buf []byte) (int, error) {
	return mc.Conn.Write(buf)
}

func recordBytesRead(bytesRead int64) {
	if bytesRead == 0 {
		return
	}

	lock.Lock()
	defer lock.Unlock()

	bytesSinceLastUpdate += float64(bytesRead)
	if lastBandwidthUpdate.IsZero() {
		lastBandwidthUpdate = time.Now()
	}

	bucketDuration := time.Since(lastBandwidthUpdate)

	if bucketDuration > maxBucketDuration {
		bps = bytesSinceLastUpdate / bucketDuration.Seconds()
		lastBandwidthUpdate = time.Now()
		bytesSinceLastUpdate = 0.0
	}
}

// GetBPS returns the last measured number of bytes transferred
// in a 1-second interval as a floating point value.
func GetBPS() float64 {
	lock.Lock()
	defer lock.Unlock()

	bucketDuration := time.Since(lastBandwidthUpdate)

	if bucketDuration > maxBucketDuration*2 {
		// if we don't read anything from the network in a while,
		// bps won't update, but it should be 0
		return 0.0
	}

	return bps
}
