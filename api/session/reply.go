// SPDX-License-Identifier: Apache-2.0

package session

import (
	"crypto/sha256"
	"encoding/base64"
	"strings"

	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// ReplySignatureHeader carries the node's signature over a handshake reply.
// Clients pin the node's self-signed certificate, which nothing on the chain
// vouches for, so without it anyone on the network path could hand a client
// another WireGuard server key or address, or read its OpenVPN key and proxy
// credentials. The signature goes in a header so the JSON body stays
// byte-identical to what clients and the network's probes already accept.
//
// Value: "secp256k1:" + base64(compressed public key) + ";" + base64(r||s).
const ReplySignatureHeader = "X-Dvpnd-Signature"

// replyDomain separates the reply signature from anything else the same key
// signs (transactions, and in the future other node statements).
const replyDomain = "dvpnd/handshake-reply/v1"

// replySigner signs with the node's signing key: the account the chain knows
// the node by, or with authz a hot key the node account granted.
type replySigner interface {
	SignBytes(msg []byte) ([]byte, cryptotypes.PubKey, error)
}

// replyDigest is the 32 bytes the node signs:
//
//	SHA-256( "dvpnd/handshake-reply/v1" || BE64(id) || SHA-256(request data)
//	         || SHA-256(reply data) || addrs joined by "\n" )
//
// where request data is the peer request the client signed (the decoded
// "data" of its body), reply data is the decoded result.data, and addrs is
// result.addrs in order. Hashing the request ties the reply to the client's
// own request; hashing each data field keeps the boundaries unambiguous.
func replyDigest(id uint64, request, reply []byte, addrs []string) []byte {
	var (
		requestHash = sha256.Sum256(request)
		replyHash   = sha256.Sum256(reply)
		h           = sha256.New()
	)

	h.Write([]byte(replyDomain))
	h.Write(sdk.Uint64ToBigEndian(id))
	h.Write(requestHash[:])
	h.Write(replyHash[:])
	h.Write([]byte(strings.Join(addrs, "\n")))

	return h.Sum(nil)
}

// signReply returns the header value for a reply. The signature is made the
// way the SDK signs (ECDSA over SHA-256 of the 32-byte digest, low-s, r||s),
// which is also how the client signed its request.
func signReply(signer replySigner, id uint64, request []byte, result *HandshakeResult) (string, error) {
	reply, err := base64.StdEncoding.DecodeString(result.Data)
	if err != nil {
		return "", err
	}

	sig, pub, err := signer.SignBytes(replyDigest(id, request, reply, result.Addrs))
	if err != nil {
		return "", err
	}

	return pubKeyPrefix + base64.StdEncoding.EncodeToString(pub.Bytes()) + ";" +
		base64.StdEncoding.EncodeToString(sig), nil
}
