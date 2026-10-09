// SPDX-License-Identifier: Apache-2.0

package api

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

// Rules: [HS-10].
func TestLimitBody(t *testing.T) {
	gin.SetMode(gin.TestMode)

	r := gin.New()
	r.Use(limitBody())
	r.POST("/", func(c *gin.Context) {
		if _, err := io.ReadAll(c.Request.Body); err != nil {
			c.Status(http.StatusRequestEntityTooLarge)
			return
		}
		c.Status(http.StatusOK)
	})

	for size, want := range map[int]int{1024: http.StatusOK, maxBodyBytes + 1: http.StatusRequestEntityTooLarge} {
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/", strings.NewReader(strings.Repeat("x", size))))
		if rec.Code != want {
			t.Errorf("body of %d bytes: %d, want %d", size, rec.Code, want)
		}
	}
}
