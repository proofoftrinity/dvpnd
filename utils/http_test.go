// SPDX-License-Identifier: Apache-2.0

package utils

import (
	"net/http"
	"testing"
)

func TestNewServerHasLimits(t *testing.T) {
	s := newServer(http.NotFoundHandler())
	if s.ReadHeaderTimeout == 0 || s.ReadTimeout == 0 || s.IdleTimeout == 0 || s.MaxHeaderBytes == 0 {
		t.Fatalf("every server limit must be set: %+v", s)
	}
}
