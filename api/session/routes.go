// SPDX-License-Identifier: Apache-2.0
// Modified from sentinel-official/dvpn-node @ 62bde16 (2024-01-25). See NOTICE.

package session

import (
	"github.com/gin-gonic/gin"

	"github.com/trinitystake/dvpnd/v9/context"
)

func RegisterRoutes(ctx *context.Context, router gin.IRouter) {
	// Both handshake routes share one attempt limit, checked after the TLS
	// requirement so a refused plain-HTTP request does not use it up.
	limit := limitHandshakes(ctx, newRateLimiter(handshakeLimit, handshakeWindow))

	// Current client apps handshake at the root path.
	router.POST("/", requireTLS(), limit, HandlerHandshake(ctx))
	// Legacy endpoint kept for older clients.
	router.POST("/accounts/:acc_address/sessions/:id", requireTLS(), limit, HandlerAddSession(ctx))
}
