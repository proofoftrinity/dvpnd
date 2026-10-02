// SPDX-License-Identifier: Apache-2.0

package types

import "net"

// TunnelHost is implemented by the services that carry clients through a
// tunnel interface (WireGuard, AmneziaWG, OpenVPN): the node's own IPv4
// address inside the tunnel, valid once Init has run. The Handshake resolver
// listens there, so only connected clients can reach it.
type TunnelHost interface {
	TunnelIPv4() net.IP
}
