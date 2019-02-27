// Package netx provides additional libraries that extend some of the behaviors
// in the net standard package.
package netx

import (
	"context"
	"net"
	"sync/atomic"
	"time"
)

var (
	dial           atomic.Value
	dialUDP        atomic.Value
	listenUDP      atomic.Value
	resolveTCPAddr atomic.Value
	resolveUDPAddr atomic.Value

	defaultDialTimeout = 1 * time.Minute
)

func init() {
	Reset()
}

// Dial is like DialTimeout using a default timeout of 1 minute.
func Dial(network string, addr string) (net.Conn, error) {
	return DialTimeout(network, addr, defaultDialTimeout)
}

// DialUDP acts like Dial but for UDP networks.
func DialUDP(network string, laddr, raddr *net.UDPAddr) (*net.UDPConn, error) {
	return dialUDP.Load().(func(string, *net.UDPAddr, *net.UDPAddr) (*net.UDPConn, error))(network, laddr, raddr)
}

// DialTimeout dials the given addr on the given net type using the configured
// dial function, timing out after the given timeout.
func DialTimeout(network string, addr string, timeout time.Duration) (net.Conn, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	conn, err := DialContext(ctx, network, addr)
	cancel()
	return conn, err
}

// DialContext dials the given addr on the given net type using the configured
// dial function, with the given context.
func DialContext(ctx context.Context, network string, addr string) (net.Conn, error) {
	return dial.Load().(func(context.Context, string, string) (net.Conn, error))(ctx, network, addr)
}

// ListenUDP acts like ListenPacket for UDP networks.
func ListenUDP(network string, laddr *net.UDPAddr) (*net.UDPConn, error) {
	return listenUDP.Load().(func(network string, laddr *net.UDPAddr) (*net.UDPConn, error))(network, laddr)
}

// OverrideDial overrides the global dial function.
func OverrideDial(dialFN func(ctx context.Context, net string, addr string) (net.Conn, error)) {
	dial.Store(dialFN)
}

// OverrideDialUDP overrides the global dialUDP function.
func OverrideDialUDP(dialFN func(net string, laddr, raddr *net.UDPAddr) (*net.UDPConn, error)) {
	dialUDP.Store(dialFN)
}

// OverrideListenUDP overrides the global listenUDP function.
func OverrideListenUDP(listenFN func(network string, laddr *net.UDPAddr) (*net.UDPConn, error)) {
	listenUDP.Store(listenFN)
}

// Resolve resolves the given tcp address using the configured resolve function.
func Resolve(network string, addr string) (*net.TCPAddr, error) {
	return resolveTCPAddr.Load().(func(string, string) (*net.TCPAddr, error))(network, addr)
}

func ResolveUDPAddr(network string, addr string) (*net.UDPAddr, error) {
	return resolveUDPAddr.Load().(func(string, string) (*net.UDPAddr, error))(network, addr)
}

// OverrideResolve overrides the global resolve function.
func OverrideResolve(resolveFN func(net string, addr string) (*net.TCPAddr, error)) {
	resolveTCPAddr.Store(resolveFN)
}

// OverrideResolveUDP overrides the global resolveUDP function.
func OverrideResolveUDP(resolveFN func(net string, addr string) (*net.UDPAddr, error)) {
	resolveUDPAddr.Store(resolveFN)
}

// Reset resets netx to its default settings
func Reset() {
	var d net.Dialer
	OverrideDial(d.DialContext)
	OverrideDialUDP(net.DialUDP)
	OverrideListenUDP(net.ListenUDP)
	OverrideResolve(net.ResolveTCPAddr)
	OverrideResolveUDP(net.ResolveUDPAddr)
}
