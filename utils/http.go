// SPDX-License-Identifier: Apache-2.0
// Modified from sentinel-official/dvpn-node @ 62bde16 (2024-01-25). See NOTICE.

package utils

import (
	"crypto/rand"
	"crypto/tls"
	"net"
	"net/http"
	"time"

	"github.com/soheilhy/cmux"
)

// Limits on the node's web server. Without them one host could hold thousands
// of connections open by sending a byte at a time (or nothing at all), or
// stream an endless request, until the node runs out of connections or
// memory. There is no write limit: a handshake waits on chain queries, each
// bounded by rpc_query_timeout, and its reply is small.
const (
	firstBytesTimeout = 10 * time.Second // to send the bytes that tell TLS from plain HTTP
	readHeaderTimeout = 10 * time.Second
	readTimeout       = 30 * time.Second
	idleTimeout       = time.Minute
	maxHeaderBytes    = 16 << 10
)

// newServer is the HTTP server for one side of the port (TLS or plain).
func newServer(handler http.Handler) *http.Server {
	return &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: readHeaderTimeout,
		ReadTimeout:       readTimeout,
		IdleTimeout:       idleTimeout,
		MaxHeaderBytes:    maxHeaderBytes,
	}
}

func ListenAndServeTLS(address, certFile, keyFile string, handler http.Handler) error {
	l, err := net.Listen("tcp", address)
	if err != nil {
		return err
	}

	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return err
	}

	var (
		mux    = cmux.New(l)
		tlsMux = mux.Match(cmux.TLS())
		anyMux = mux.Match(cmux.Any())
	)
	mux.SetReadTimeout(firstBytesTimeout)

	go func() {
		if err := newServer(handler).Serve(
			tls.NewListener(
				tlsMux,
				&tls.Config{
					Certificates: []tls.Certificate{
						cert,
					},
					Rand: rand.Reader,
				},
			),
		); err != nil {
			panic(err)
		}
	}()

	go func() {
		if err := newServer(handler).Serve(anyMux); err != nil {
			panic(err)
		}
	}()

	return mux.Serve()
}
