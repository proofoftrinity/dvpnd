// SPDX-License-Identifier: Apache-2.0

package types

import (
	"crypto/sha256"
	"encoding/hex"
)

// KeyTag is how a peer key appears in the node's logs and error replies: the
// first 6 bytes of its SHA-256, in hex. For the proxy protocols the key holds
// the client's UUID, which is its login credential, so the key itself is never
// written out. The tag is stable, so the lines about one peer can still be
// followed.
func KeyTag(key string) string {
	sum := sha256.Sum256([]byte(key))

	return hex.EncodeToString(sum[:6])
}
