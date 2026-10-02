// SPDX-License-Identifier: Apache-2.0

package utils

import (
	"bytes"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	cmtlog "github.com/cometbft/cometbft/libs/log"
)

func TestNewServerHasLimits(t *testing.T) {
	s := newServer(http.NotFoundHandler(), cmtlog.NewNopLogger())
	if s.ReadHeaderTimeout == 0 || s.ReadTimeout == 0 || s.IdleTimeout == 0 || s.MaxHeaderBytes == 0 || s.ErrorLog == nil {
		t.Fatalf("every server limit and the error log must be set: %+v", s)
	}
}

// syncBuffer is a bytes.Buffer the server's goroutines can write to while the
// test reads it.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()

	return b.buf.String()
}

// TestServerMessagesGoToTheDebugLog: a connection that fails its TLS handshake
// makes the HTTP server log the remote address. That line must reach the
// node's logger at debug level, not stderr, so a node at the default level
// keeps no client addresses.
func TestServerMessagesGoToTheDebugLog(t *testing.T) {
	var buf syncBuffer
	srv := httptest.NewUnstartedServer(http.NotFoundHandler())
	srv.Config = newServer(http.NotFoundHandler(), cmtlog.NewTMLogger(&buf))
	srv.StartTLS()
	defer srv.Close()

	conn, err := net.Dial("tcp", srv.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	_, _ = conn.Write([]byte("GET / HTTP/1.1\r\nHost: x\r\n\r\n")) // plain HTTP to the TLS side
	_ = conn.Close()

	for deadline := time.Now().Add(3 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		if strings.Contains(buf.String(), "TLS handshake error") {
			break
		}
	}
	out := buf.String()
	if !strings.Contains(out, "TLS handshake error") {
		t.Fatalf("the handshake error must reach the node's logger: %q", out)
	}
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if !strings.HasPrefix(line, "D[") {
			t.Fatalf("the server's own messages are debug lines only: %q", line)
		}
	}
}
