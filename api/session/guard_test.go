// SPDX-License-Identifier: Apache-2.0

package session

import (
	"bytes"
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	cmtlog "github.com/cometbft/cometbft/libs/log"
	"github.com/gin-gonic/gin"

	"github.com/trinitystake/dvpnd/v9/context"
)

func TestRequireTLS(t *testing.T) {
	gin.SetMode(gin.TestMode)

	r := gin.New()
	r.POST("/", requireTLS(), func(c *gin.Context) { c.Status(http.StatusOK) })

	plain := httptest.NewRequest(http.MethodPost, "/", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, plain)
	if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), `"code":11`) {
		t.Fatalf("plain HTTP handshake: %d %s", rec.Code, rec.Body)
	}

	secure := httptest.NewRequest(http.MethodPost, "/", nil)
	secure.TLS = &tls.ConnectionState{}
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, secure)
	if rec.Code != http.StatusOK {
		t.Fatalf("TLS handshake must pass: %d %s", rec.Code, rec.Body)
	}
}

func TestRateLimiter(t *testing.T) {
	now := time.Unix(1000, 0)
	l := newRateLimiter(3, time.Minute)
	l.now = func() time.Time { return now }

	for i := 0; i < 3; i++ {
		if ok, _, _ := l.allow("a"); !ok {
			t.Fatalf("attempt %d within the limit was refused", i+1)
		}
	}
	ok, wait, first := l.allow("a")
	if ok || !first || wait != time.Minute {
		t.Fatalf("over the limit: ok %v wait %v first %v", ok, wait, first)
	}
	now = now.Add(20 * time.Second)
	if ok, wait, first := l.allow("a"); ok || first || wait != 40*time.Second {
		t.Fatalf("second refusal: ok %v wait %v first %v", ok, wait, first)
	}
	if ok, _, _ := l.allow("b"); !ok {
		t.Fatal("another client block has its own count")
	}

	now = now.Add(40 * time.Second)
	if ok, _, _ := l.allow("a"); !ok {
		t.Fatal("a new window starts the count again")
	}
	if len(l.counts) != 1 {
		t.Fatalf("a new window drops the old counts: %v", l.counts)
	}
}

func TestClientBlock(t *testing.T) {
	for in, want := range map[string]string{
		"203.0.113.9":          "203.0.113.9",
		"::ffff:203.0.113.9":   "203.0.113.9",
		"2001:db8:1:2:aaaa::1": "2001:db8:1:2::",
		"not-an-address":       "not-an-address",
	} {
		if got := clientBlock(in); got != want {
			t.Errorf("clientBlock(%q) = %q, want %q", in, got, want)
		}
	}
	if clientBlock("2001:db8:1:2::1") != clientBlock("2001:db8:1:2:dead:beef::7") {
		t.Fatal("addresses in one /64 must share a count")
	}
}

func TestLimitHandshakes(t *testing.T) {
	gin.SetMode(gin.TestMode)

	var buf bytes.Buffer
	ctx := context.NewContext().WithLogger(cmtlog.NewTMLogger(&buf))
	r := gin.New()
	r.POST("/", limitHandshakes(ctx, newRateLimiter(2, time.Minute)), func(c *gin.Context) { c.Status(http.StatusOK) })

	serve := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/", nil)
		req.RemoteAddr = "203.0.113.9:4242"
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		return rec
	}

	for i := 0; i < 2; i++ {
		if rec := serve(); rec.Code != http.StatusOK {
			t.Fatalf("attempt %d: %d", i+1, rec.Code)
		}
	}
	for i := 0; i < 3; i++ {
		rec := serve()
		if rec.Code != http.StatusTooManyRequests || rec.Header().Get("Retry-After") == "" || !strings.Contains(rec.Body.String(), `"code":12`) {
			t.Fatalf("over the limit: %d %q %s", rec.Code, rec.Header().Get("Retry-After"), rec.Body)
		}
	}
	if n := strings.Count(buf.String(), "Refusing handshake attempts"); n != 1 {
		t.Fatalf("limiting is logged once per window, got %d lines:\n%s", n, buf.String())
	}
	if strings.Contains(buf.String(), "203.0.113.9") {
		t.Fatalf("the limiter must not log client addresses:\n%s", buf.String())
	}
}
