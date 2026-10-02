// SPDX-License-Identifier: Apache-2.0

package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// maxBodyBytes caps a request body. A handshake is a few hundred bytes of
// JSON; without a cap a client could make the node read an endless body.
const maxBodyBytes = 64 << 10

func limitBody() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxBodyBytes)
		c.Next()
	}
}
