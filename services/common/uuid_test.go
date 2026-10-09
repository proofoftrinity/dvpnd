// SPDX-License-Identifier: Apache-2.0

package common

import (
	"strings"
	"testing"
)

// A refusal has to explain itself in the node's log. The case that prompted
// this: a client that only speaks WireGuard reaches a Hysteria2 node and sends
// public_key, and "uuid is missing" alone left the operator to work that out.
//
// Rules: [HS-11].
func TestUUIDPeerRequestExplainsTheProtocolMismatch(t *testing.T) {
	cases := []struct {
		name     string
		protocol string
		raw      string
		want     []string // every fragment the message must carry
	}{
		{
			name:     "a wireguard client reaches a proxy node",
			protocol: "hysteria2",
			raw:      `{"public_key":"kEQ0b1BsUmFJc0dRZDlkZmpMcXhNZDFvUGZ4NkNqSjE="}`,
			want:     []string{"this node runs hysteria2", "public_key", "wireguard client"},
		},
		{
			name:     "no recognisable field at all",
			protocol: "xray",
			raw:      `{}`,
			want:     []string{"this node runs xray", "uuid", "none was sent"},
		},
		{
			name:     "uuid present but the wrong length",
			protocol: "v2ray",
			raw:      `{"uuid":[1,2,3]}`,
			want:     []string{"invalid v2ray peer request", "16 bytes"},
		},
		{
			name:     "uuid present but not a uuid",
			protocol: "openvpn",
			raw:      `{"uuid":"not-a-uuid"}`,
			want:     []string{"invalid openvpn peer request", "not-a-uuid"},
		},
		{
			name:     "not JSON",
			protocol: "hysteria2",
			raw:      `{`,
			want:     []string{"invalid hysteria2 peer request"},
		},
	}
	for _, c := range cases {
		_, err := UUIDPeerRequest(c.protocol, []byte(c.raw))
		if err == nil {
			t.Errorf("%s: expected an error", c.name)
			continue
		}
		for _, want := range c.want {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("%s: %q does not mention %q", c.name, err, want)
			}
		}
	}
}

// Rules: [CT-5].
func TestUUIDPeerRequestAcceptsBothForms(t *testing.T) {
	const canonical = "3f2504e0-4f89-41d3-9a0c-0305e82c3301"

	fromString, err := UUIDPeerRequest("hysteria2", []byte(`{"uuid":"`+canonical+`"}`))
	if err != nil {
		t.Fatal(err)
	}
	if FormatUUID(fromString) != canonical {
		t.Fatalf("string form: got %s", FormatUUID(fromString))
	}

	fromBytes, err := UUIDPeerRequest("v2ray", []byte(`{"uuid":[63,37,4,224,79,137,65,211,154,12,3,5,232,44,51,1]}`))
	if err != nil {
		t.Fatal(err)
	}
	if fromBytes != fromString {
		t.Fatalf("byte form gave %s, want %s", FormatUUID(fromBytes), canonical)
	}
}
