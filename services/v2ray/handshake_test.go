// SPDX-License-Identifier: Apache-2.0

package v2ray

import (
	"encoding/binary"
	"encoding/json"
	"testing"
)

// Rules: [CT-5].
func TestParsePeerRequest(t *testing.T) {
	s := NewV2Ray()

	data, err := s.ParsePeerRequest([]byte(`{"uuid":[1,2,3,4,5,6,7,8,9,10,11,12,13,14,15,16]}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(data) != 17 || data[0] != 0x01 || data[1] != 1 || data[16] != 16 {
		t.Fatalf("array uuid: %x", data)
	}

	data, err = s.ParsePeerRequest([]byte(`{"uuid":"01020304-0506-0708-090a-0b0c0d0e0f10"}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(data) != 17 || data[0] != 0x01 || data[1] != 1 || data[16] != 16 {
		t.Fatalf("string uuid: %x", data)
	}

	if _, err := s.ParsePeerRequest([]byte(`{"uuid":42}`)); err == nil {
		t.Error("bad uuid accepted")
	}
}

// Rules: [CT-3], [CT-4].
func TestMetadataAndPayload(t *testing.T) {
	s := NewV2Ray()
	binary.BigEndian.PutUint16(s.info, 8443)
	s.info[2] = 0x01 // tcp
	s.info[3] = 0    // no TLS

	in := s.inbound()
	if in.Port != 8443 || in.ProxyProtocol != 2 || in.TransportProtocol != 1 || in.TransportSecurity != 1 || in.TLSPin != "" {
		t.Fatalf("plain inbound: %+v", in)
	}

	s.info[3] = 1
	s.tlsPin = "abc"
	if pub, _ := json.Marshal(s.PublicMetadata()); string(pub) != `[{"port":"","proxy_protocol":2,"transport_protocol":1,"transport_security":2,"tls_pin":""}]` {
		t.Fatalf("public listing must blank the port and pin: %s", pub)
	}

	payload, err := s.HandshakePayload(nil)
	if err != nil {
		t.Fatal(err)
	}
	out, _ := json.Marshal(payload)
	want := `{"metadata":[{"port":8443,"proxy_protocol":2,"transport_protocol":1,"transport_security":2,"tls_pin":"abc"}]}`
	if string(out) != want {
		t.Fatalf("payload:\n got %s\nwant %s", out, want)
	}
	if s.Name() != "v2ray" {
		t.Fatalf("name: %s", s.Name())
	}
}
