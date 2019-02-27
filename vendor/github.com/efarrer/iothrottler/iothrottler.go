// Copyright 2012 Evan Farrer. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

/*
	Package iothrottler implements application IO throttling.
*/

package iothrottler

import (
	"errors"
	"io"
	"log"
	"math"
	"net"
	"time"
)

// The Bandwidth type represents a bandwidth quantity in bytes per second.
// Sub-byte per seconds values are not supported
type Bandwidth int64

const (
	// Bytes per second
	BytesPerSecond Bandwidth = 1
	// Kilobits per second
	Kbps = BytesPerSecond * (1024 / 8)
	// Megabits per second
	Mbps = Kbps * 1024
	// Gigabits per second
	Gbps = Mbps * 1024
	// Unlimited bandwidth
	Unlimited = math.MaxInt64
)

// A pool for throttling IO
type IOThrottlerPool struct {
	// A channel for setting the pools bandwith
	bandwidthSettingChan chan Bandwidth
	// A channel for allocating bandwidth
	bandwidthAllocatorChan chan Bandwidth
	// A channel for returning unused bandwidth to server
	bandwidthFreeChan chan Bandwidth
	// A channel for getting a count of the clients
	// A pool only accumulates bandwidth if the pool is non-empty
	clientCountChan chan int64
	// A channel for getting pool release messages
	releasePoolChan chan bool
}

// Construct a new IO throttling pool
// The bandwidth for this pool will be limited to 'bandwidth'
func NewIOThrottlerPool(bandwidth Bandwidth) *IOThrottlerPool {

	pool := &IOThrottlerPool{make(chan Bandwidth), make(chan Bandwidth), make(chan Bandwidth), make(chan int64), make(chan bool)}

	go throttlerPoolDriver(pool)

	pool.bandwidthSettingChan <- bandwidth

	return pool
}

func throttlerPoolDriver(pool *IOThrottlerPool) {

	// These will all be recalculated as soon as the bandwidth is set
	clientCount := int64(0)
	currentBandwidth := Bandwidth(0)
	totalbandwidth := Bandwidth(0)
	allocationSize := Bandwidth(0)
	var timeout <-chan time.Time = nil
	var thisBandwidthAllocatorChan chan Bandwidth = nil

	recalculateAllocationSize := func() {
		if currentBandwidth == Unlimited {
			totalbandwidth = Unlimited
		}

		if totalbandwidth == Unlimited {
			allocationSize = Unlimited
		} else {

			// Calculate how much bandwidth each consumer will get
			// We divvy the available bandwidth among the existing
			// clients but leave a bit of room in case more clients
			// connect in the mean time. This greatly improves
			// performance
			if clientCount == 0 {
				allocationSize = totalbandwidth / 2
			} else {
				allocationSize = totalbandwidth / Bandwidth(clientCount*2)
			}

			// Even if we have a negative totalbandwidth we never want to
			// allocate negative bandwidth to members of our pool
			if allocationSize < 0 {
				allocationSize = 0
			}

			// If we do have some bandwidth make sure we at least allocate 1 byte
			if allocationSize == 0 && totalbandwidth > 0 {
				allocationSize = 1
			}
		}

		if allocationSize > 0 {
			// Since we have bandwidth to allocate we can select on
			// the bandwidth allocator chan
			thisBandwidthAllocatorChan = pool.bandwidthAllocatorChan
		} else {
			// We've allocate all out bandwidth so we need to wait for
			// more
			thisBandwidthAllocatorChan = nil
		}
	}

	for {
		select {
		// Release the pool
		case release := <-pool.releasePoolChan:
			if release {
				close(pool.bandwidthAllocatorChan)
				close(pool.bandwidthFreeChan)
				// Don't close the clientCountChan it's not needed and it
				// complicates the code (two different functions need to recover
				// the panic if it's closed
				pool.releasePoolChan <- true
				close(pool.releasePoolChan)
				close(pool.clientCountChan)
				return
			}

		// Register a new client
		case increment := <-pool.clientCountChan:
			// We got our first client
			// We start the timer as soon as we get our first client
			if clientCount == 0 {
				timeout = time.Tick(time.Second * 1)
			}
			clientCount += increment
			// Our last client left so stop the timer
			if clientCount == 0 {
				timeout = nil
			}
			recalculateAllocationSize()

		// Set the new bandwidth
		case newBandwidth := <-pool.bandwidthSettingChan:
			// If we've accumulated more bandwidth then the new amount we
			// truncate the totalbandwidth to the new set amount. This is
			// important if the totalbandwidth is much larger than the
			// new bandwidth value we could end up not really respecting the
			// new bandwidth setting. An extreme example of this is if the
			// old bandwidth was set to Unlimited (totalbandwidth would be
			// Unlimited)
			//
			// If the totalbandwidth is less than the new bandwidth setting
			// we want to bring it up to the new bandwidth value so clients
			// can immediately use the new available bandwidth
			totalbandwidth = newBandwidth

			// Update the current bandwidth
			currentBandwidth = newBandwidth

			recalculateAllocationSize()

		// Allocate some bandwidth
		case thisBandwidthAllocatorChan <- allocationSize:
			if Unlimited != totalbandwidth {
				totalbandwidth -= allocationSize

				recalculateAllocationSize()
			}

		// Get unused bandwidth back from client
		case returnSize := <-pool.bandwidthFreeChan:
			if Unlimited != totalbandwidth {
				totalbandwidth += returnSize
			}

			recalculateAllocationSize()

		// Get more bandwidth to allocate
		case <-timeout:
			if clientCount > 0 {
				if Unlimited != totalbandwidth {
					// Get a new allotment of bandwidth
					totalbandwidth += currentBandwidth

					recalculateAllocationSize()
				}
			}
		}
	}
}

// Release the IOThrottlerPool all bandwidth
func (pool *IOThrottlerPool) ReleasePool() {
	// If pool.releasePoolChan is already closed (called ReleasePool more than
	// once) then sending to it will panic so just swallow the panic
	defer func() {
		recover()
	}()
	pool.releasePoolChan <- true
	<-pool.releasePoolChan
}

// Sets the IOThrottlerPool's bandwith rate
func (pool *IOThrottlerPool) SetBandwidth(bandwith Bandwidth) {
	pool.bandwidthSettingChan <- bandwith
}

// Returns the first error or nil if neither are errors
func orErrors(er0, er1 error) error {
	if er0 != nil {
		return er0
	}
	return er1
}

/*
 * Updates the client count for a pool return error on failure
 */
func twiddleClientCount(p *IOThrottlerPool, change int64) (err error) {
	// When the pool has been released the server closes clientCountChan
	// so our channel send will panic. We want to set the return error
	defer func() {
		if r := recover(); r != nil {
			err = errors.New("Pool has been released")
		}
	}()
	// Update the client count
	p.clientCountChan <- change

	return nil
}

// A ReadCloser that will respect the bandwidth limitations of the IOThrottlerPool
type throttledReadCloser struct {
	origReadCloser io.ReadCloser
	pool           *IOThrottlerPool
}

// A WriteCloser that will respect the bandwidth limitations of the IOThrottlerPool
type throttledWriteCloser struct {
	origWriteCloser io.WriteCloser
	pool            *IOThrottlerPool
}

// A ReadWriteCloser that will respect the bandwidth limitations of the IOThrottlerPool
type throttledReadWriteCloser struct {
	throttledReadCloser
	throttledWriteCloser
}

// Read method for the throttledReadCloser
func (t *throttledReadCloser) Read(b []byte) (int, error) {
	// Get some bandwidth
	allocation, ok := <-t.pool.bandwidthAllocatorChan
	if !ok {
		// Pool has been released
		return 0, errors.New("Pool has been released")
	}

	// Calculate how much we can read
	toRead := Bandwidth(len(b))
	if allocation < Bandwidth(len(b)) {
		toRead = allocation
	}

	// Do the limited read
	n, err := t.origReadCloser.Read(b[:toRead])

	// Free up what we didn't use
	if Bandwidth(n) < allocation && allocation != Unlimited {
		t.pool.bandwidthFreeChan <- allocation - Bandwidth(n)
	}

	return n, err
}

// Write method for the throttledWriteCloser
func (t *throttledWriteCloser) Write(data []byte) (int, error) {

	// Write must either write len(data) bytes or return an error
	allocation := Bandwidth(0)

	for allocation < Bandwidth(len(data)) {
		// Get some bandwidth
		thisAllocation, ok := <-t.pool.bandwidthAllocatorChan
		if !ok {
			// Pool has been released
			return 0, errors.New("Pool has been released")
		}
		allocation += thisAllocation
	}

	// Do the write
	n, err := t.origWriteCloser.Write(data)

	// Free up what we didn't use
	if Bandwidth(n) < allocation && allocation != Unlimited {
		t.pool.bandwidthFreeChan <- allocation - Bandwidth(n)
	}

	return n, err
}

// Close method for the throttledReadCloser
func (t *throttledReadCloser) Close() error {
	// Unregister with the pool
	err := twiddleClientCount(t.pool, -1)

	return orErrors(err, t.origReadCloser.Close())
}

// Close method for the throttledWriteCloser
func (t *throttledWriteCloser) Close() error {
	// Unregister with the pool
	err := twiddleClientCount(t.pool, -1)

	return orErrors(err, t.origWriteCloser.Close())
}

// Close method for the throttledReadWriteCloser
func (t *throttledReadWriteCloser) Close() error {
	// In this case we really have two copies of all the data
	// It really doesn't matter which we use as the reader and writer hold the
	// same data

	// Unregister with the pool
	err := twiddleClientCount(t.throttledReadCloser.pool, -1)

	return orErrors(err, t.throttledReadCloser.origReadCloser.Close())
}

// Add a io.ReadCloser to the pool. The returned io.ReadCloser shares the
// IOThrottlerPool's bandwidth with other items in the pool.
func (p *IOThrottlerPool) AddReader(reader io.ReadCloser) (io.ReadCloser, error) {
	// Register with the pool
	err := twiddleClientCount(p, 1)
	if err != nil {
		return nil, err
	}

	return &throttledReadCloser{reader, p}, nil
}

// Add a io.WriteCloser to the pool. The returned io.WriteCloser shares the
// IOThrottlerPool's bandwidth with other items in the pool.
func (p *IOThrottlerPool) AddWriter(writer io.WriteCloser) (io.WriteCloser, error) {
	// Register with the pool
	err := twiddleClientCount(p, 1)
	if err != nil {
		return nil, err
	}

	return &throttledWriteCloser{writer, p}, nil
}

// Add a io.ReadWriteCloser to the pool. The returned io.ReadWriteCloser shares the
// IOThrottlerPool's bandwidth with other items in the pool.
func (p *IOThrottlerPool) AddReadWriter(readWriter io.ReadWriteCloser) (io.ReadWriteCloser, error) {
	// Register with the pool
	err := twiddleClientCount(p, 1)
	if err != nil {
		return nil, err
	}

	return &throttledReadWriteCloser{throttledReadCloser{readWriter, p},
		throttledWriteCloser{readWriter, p}}, err
}

// Add a net.Conn to the pool. The returned net.Conn shares the
// IOThrottlerPool's bandwidth with other items in the pool.
type throttledConn struct {
	throttledReadWriteCloser
	originalConn net.Conn
}

// Implements the net.Conn LocalAddr method
func (c *throttledConn) LocalAddr() net.Addr {
	return c.originalConn.LocalAddr()
}

// Implements the net.Conn RemoteAddr method
func (c *throttledConn) RemoteAddr() net.Addr {
	return c.originalConn.RemoteAddr()
}

// Implements the net.Conn SetDeadline method
func (c *throttledConn) SetDeadline(t time.Time) error {
	return c.originalConn.SetDeadline(t)
}

// Implements the net.Conn SetReadDeadline method
func (c *throttledConn) SetReadDeadline(t time.Time) error {
	return c.originalConn.SetReadDeadline(t)
}

// Implements the net.Conn SetWriteDeadline method
func (c *throttledConn) SetWriteDeadline(t time.Time) error {
	return c.originalConn.SetWriteDeadline(t)
}

// Restrict the network connection to the bandwidth limitations of the IOThrottlerPool
func (p *IOThrottlerPool) AddConn(conn net.Conn) (net.Conn, error) {

	rwCloser, err := p.AddReadWriter(conn)
	if err != nil {
		return nil, err
	}
	throttledRWC, ok := rwCloser.(*throttledReadWriteCloser)
	if !ok {
		log.Fatalf("Programming error, expecting *throttledReadWriteCloser but got %v", rwCloser)
	}

	return &throttledConn{*throttledRWC, conn}, nil
}
