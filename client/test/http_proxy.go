// Copyright 2022 Northern.tech AS
//
//    Licensed under the Apache License, Version 2.0 (the "License");
//    you may not use this file except in compliance with the License.
//    You may obtain a copy of the License at
//
//        http://www.apache.org/licenses/LICENSE-2.0
//
//    Unless required by applicable law or agreed to in writing, software
//    distributed under the License is distributed on an "AS IS" BASIS,
//    WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//    See the License for the specific language governing permissions and
//    limitations under the License.

package test

import (
	"bufio"
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"

	"github.com/mendersoftware/mender/client"

	"github.com/pkg/errors"
)

func init() {
	client.ProxyURLFromHostPortGetter = func(addr string) (*url.URL, error) {
		p := os.Getenv("HTTPS_PROXY")
		if p == "" {
			return nil, nil
		}
		return url.Parse(p)
	}
}

const (
	inetAddrLoopback = "127.0.0.1:0"
	username         = "user"
	password         = "password"
)

type connection struct {
	clientConn net.Conn
	serverConn net.Conn
	wg         *sync.WaitGroup
}

type TestHttpProxy struct {
	isRunning          bool
	listener           net.Listener
	quit               chan interface{}
	wg                 *sync.WaitGroup
	errors             []error
	connections        map[*connection]bool
	mut                *sync.Mutex
	requireAuth        bool
	numRequiredConns   int
	numSuccessfulConns int
}

func NewTestHttpProxy(
	numRequiredConns int,
	requireAuth bool,
) (*TestHttpProxy, error) {
	listener, err := net.Listen("tcp", inetAddrLoopback)
	if err != nil {
		return nil, err
	}
	p := &TestHttpProxy{
		isRunning:        true,
		listener:         listener,
		quit:             make(chan interface{}),
		wg:               new(sync.WaitGroup),
		errors:           make([]error, 0),
		connections:      make(map[*connection]bool),
		mut:              &sync.Mutex{},
		requireAuth:      requireAuth,
		numRequiredConns: numRequiredConns,
	}
	p.wg.Add(1)
	go p.serve()
	return p, nil
}

func (p *TestHttpProxy) GetUrl() string {
	if !p.requireAuth {
		return "http://" + p.listener.Addr().String()
	}
	return "http://" + username + ":" + password + "@" + p.listener.Addr().String()
}

func (p *TestHttpProxy) Stop() error {
	if p == nil {
		return nil
	}
	close(p.quit)
	p.listener.Close()
	p.mut.Lock()
	p.isRunning = false
	for c := range p.connections {
		c.clientConn.Close()
		c.serverConn.Close()
	}
	p.mut.Unlock()
	p.wg.Wait()

	if len(p.errors) > 0 {
		return p.errors[0]
	}

	if p.numRequiredConns >= 0 && p.numRequiredConns != p.numSuccessfulConns {
		return errors.Errorf(
			"number of successful connections %d "+
				"was not equal to required amount of %d",
			p.numSuccessfulConns,
			p.numRequiredConns)
	}

	return nil
}

func (p *TestHttpProxy) serve() {
	defer p.wg.Done()

	for {
		conn, err := p.listener.Accept()
		if err != nil {
			select {
			case <-p.quit:
				return
			default:
				p.appendError(errors.Wrap(err, "failed to accept connection"))
				return
			}
		} else {
			p.wg.Add(1)
			go func() {
				defer p.wg.Done()
				p.handleConnection(conn)
			}()
		}
	}
}

func (p *TestHttpProxy) handleConnection(conn net.Conn) {
	defer conn.Close()
	br := bufio.NewReader(conn)
	req, err := http.ReadRequest(br)
	if err != nil {
		p.appendError(errors.Wrap(err, "failed to read http request"))
		return
	}

	if req.Method != http.MethodConnect {
		if err := sendResponse(conn, http.StatusBadRequest); err != nil {
			p.appendError(err)
		}
		p.appendError(errors.New("initial http request method was not CONNECT"))
		return
	}

	if err = p.handleAuth(req); err != nil {
		if sendErr := sendResponse(conn,
			http.StatusProxyAuthRequired); sendErr != nil {
			p.appendError(sendErr)
		}
		p.appendError(err)
		return
	}

	forwardConn, err := net.Dial("tcp", req.Host)
	if err != nil {
		p.appendError(errors.New("failed to dial forwarding target"))
		return
	}
	defer forwardConn.Close()

	if err := sendResponse(conn, http.StatusOK); err != nil {
		p.appendError(err)
		return
	}

	c := &connection{
		clientConn: conn,
		serverConn: forwardConn,
		wg:         new(sync.WaitGroup),
	}

	p.mut.Lock()
	p.numSuccessfulConns++
	if !p.isRunning {
		p.mut.Unlock()
		return
	}
	p.connections[c] = true
	p.mut.Unlock()

	c.wg.Add(2)
	go forwardConnection(c.wg, c.clientConn, c.serverConn)
	go forwardConnection(c.wg, c.serverConn, c.clientConn)
	c.wg.Wait()

	p.mut.Lock()
	delete(p.connections, c)
	p.mut.Unlock()
}
func (p *TestHttpProxy) handleAuth(req *http.Request) error {
	if !p.requireAuth {
		return nil
	}
	if h := req.Header["Proxy-Authorization"]; h != nil {
		h = strings.Split(h[0], " ")
		if len(h) != 2 {
			return errors.New("received auth header with bad format")
		}
		if t := h[0]; t != "Basic" {
			return errors.Errorf("received auth header with non-Basic type '%s'", t)
		}
		bytes, err := base64.StdEncoding.DecodeString(h[1])
		if err != nil {
			return errors.New("failed to base64 decode credentials")
		}
		creds := strings.Split(string(bytes), ":")
		if len(creds) != 2 {
			return errors.New("received credentials in bad format")
		}
		if creds[0] != username || creds[1] != password {
			return errors.New("username or password did not match")
		}
	} else {
		return errors.New("required auth header absent from connect request")
	}
	return nil
}

func (p *TestHttpProxy) appendError(err error) {
	p.mut.Lock()
	p.errors = append(p.errors, err)
	p.mut.Unlock()
}

func sendResponse(conn net.Conn, statusCode int) error {
	if err := (&http.Response{
		ProtoMajor: 1,
		ProtoMinor: 1,
		StatusCode: statusCode,
	}).Write(conn); err != nil {
		return errors.Wrap(err,
			fmt.Sprintf("failed to write '%d: %s; back to client",
				statusCode,
				http.StatusText(statusCode)))
	}
	return nil
}

func forwardConnection(wg *sync.WaitGroup, src net.Conn, dst net.Conn) {
	_, _ = io.Copy(dst, src)
	src.Close()
	dst.Close()
	wg.Done()
}
