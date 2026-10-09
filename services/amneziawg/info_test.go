// SPDX-License-Identifier: Apache-2.0

package amneziawg

import (
	"encoding/binary"
	"testing"
)

// TestInfoCarriesThePort: the legacy endpoint hands Info() to old clients,
// which read the listen port from its first two bytes, big-endian.
//
// Rules: [CT-6].
func TestInfoCarriesThePort(t *testing.T) {
	_, cfg, s := setup(t)
	if err := s.Init(home(t, cfg)); err != nil {
		t.Fatal(err)
	}
	if info := s.Info(); len(info) < 2 || binary.BigEndian.Uint16(info) != cfg.ListenPort {
		t.Fatalf("Info() = %x, want the default tier port %d first", info, cfg.ListenPort)
	}
}
