// SPDX-License-Identifier: Apache-2.0

package cmd

import (
	"net"
	"strings"
	"testing"
)

// TestHnsdArgs: the resolver listens on the tunnel address only, never on
// every address, and keeps no log.
func TestHnsdArgs(t *testing.T) {
	got := strings.Join(hnsdArgs(8, net.IPv4(10, 8, 0, 1)), " ")
	if got != "--log-file /dev/null --pool-size 8 --rs-host 10.8.0.1:53" {
		t.Fatalf("hnsd args: %s", got)
	}
}
