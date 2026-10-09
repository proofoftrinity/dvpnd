// SPDX-License-Identifier: Apache-2.0

package cmd

import (
	"net"
	"strings"
	"testing"
)

// TestHnsdArgs: the resolver listens on the tunnel address only, never on
// every address, and keeps no log.
//
// Rules: [PV-8].
func TestHnsdArgs(t *testing.T) {
	got := strings.Join(hnsdArgs(8, net.IPv4(10, 8, 0, 1)), " ")
	if got != "--log-file /dev/null --pool-size 8 --rs-host 10.8.0.1:53" {
		t.Fatalf("hnsd args: %s", got)
	}
}

// TestAPIPort: the port tunnel clients may still reach on the node, taken
// from the API's listen address.
//
// Rules: [EG-8].
func TestAPIPort(t *testing.T) {
	for in, want := range map[string]uint16{
		"0.0.0.0:8585": 8585,
		"[::]:443":     443,
		":7777":        7777,
		"0.0.0.0":      0,
		"0.0.0.0:http": 0,
		"":             0,
	} {
		if got := apiPort(in); got != want {
			t.Errorf("apiPort(%q) = %d, want %d", in, got, want)
		}
	}
}
