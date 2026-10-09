// SPDX-License-Identifier: Apache-2.0

package main

import (
	"encoding/base64"
	"encoding/hex"
	"strings"
	"testing"

	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	base "github.com/sentinel-official/sentinelhub/v12/types"
)

// The vector api/session's TestReplyDigestVector pins for the node.
//
// Rules: [HS-6].
func TestReplyDigestVector(t *testing.T) {
	got := replyDigest(42, []byte(`{"public_key":"abc"}`),
		[]byte(`{"addrs":["10.8.0.2/32"],"metadata":[{"port":51820}]}`),
		[]string{"203.0.113.1", "node.example"})
	if want := "8146f1e4b375b1e9ff799ccc91dc8304678a645c5e35b94575d33edd6d364882"; hex.EncodeToString(got) != want {
		t.Fatalf("digest %x, want %s", got, want)
	}
}

func TestCheckReplySignature(t *testing.T) {
	key := secp256k1.GenPrivKey()
	node := base.NodeAddress(key.PubKey().Address())
	request, reply, addrs := []byte(`{"uuid":"x"}`), []byte(`{"metadata":[]}`), []string{"203.0.113.1"}

	sig, err := key.Sign(replyDigest(7, request, reply, addrs))
	if err != nil {
		t.Fatal(err)
	}
	header := "secp256k1:" + base64.StdEncoding.EncodeToString(key.PubKey().Bytes()) + ";" +
		base64.StdEncoding.EncodeToString(sig)

	if signer, err := checkReplySignature(header, 7, request, reply, addrs, node); err != nil || signer != "the node account" {
		t.Fatalf("genuine reply: %q, %v", signer, err)
	}
	other := base.NodeAddress(secp256k1.GenPrivKey().PubKey().Address())
	if signer, err := checkReplySignature(header, 7, request, reply, addrs, other); err != nil || !strings.HasPrefix(signer, "hot key") {
		t.Fatalf("another node's key: %q, %v", signer, err)
	}
	if _, err := checkReplySignature(header, 7, request, reply, []string{"198.51.100.1"}, node); err == nil {
		t.Fatal("a swapped address verified")
	}
	if _, err := checkReplySignature("", 7, request, reply, addrs, node); err == nil {
		t.Fatal("a reply without a signature passed")
	}
}
