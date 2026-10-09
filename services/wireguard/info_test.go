// SPDX-License-Identifier: Apache-2.0

package wireguard

import (
	"encoding/binary"
	"testing"
)

// TestInfoCarriesThePort: the legacy endpoint hands Info() to old clients,
// which read the listen port from its first two bytes, big-endian.
//
// Rules: [CT-6].
func TestInfoCarriesThePort(t *testing.T) {
	s, _ := startRig(t, "wg0", false)
	if info := s.Info(); len(info) < 2 || binary.BigEndian.Uint16(info) != 51820 {
		t.Fatalf("Info() = %x, want the port 51820 first", info)
	}
}
