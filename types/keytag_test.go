// SPDX-License-Identifier: Apache-2.0

package types

import (
	"strings"
	"testing"
)

func TestKeyTag(t *testing.T) {
	const key = "AWJjZTVmY2Y0LTY0MjktNDQ5ZS1hMzZmLWU4NmVjZjVhYzk0Nw=="

	tag := KeyTag(key)
	if len(tag) != 12 || strings.Contains(key, tag) {
		t.Fatalf("tag %q must be 12 hex characters and not part of the key", tag)
	}
	if KeyTag(key) != tag {
		t.Fatal("the tag must be stable, so log lines about one peer can be followed")
	}
	if KeyTag(key+"x") == tag {
		t.Fatal("different keys must get different tags")
	}
}
