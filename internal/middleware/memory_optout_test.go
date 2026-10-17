package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestMemoryOptOutHeaderDisablesMemoryForTheRequest(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cases := map[string]bool{"": true, "on": true, "off": false, "OFF": false, "0": false, "false": false}
	for header, allowed := range cases {
		r := gin.New()
		r.Use(MemoryOptOut())
		var got bool
		r.GET("/", func(c *gin.Context) { got = types.MemoryAllowedForAgent(c.Request.Context()) })
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		if header != "" {
			req.Header.Set(MemoryOptOutHeader, header)
		}
		r.ServeHTTP(httptest.NewRecorder(), req)
		require.Equal(t, allowed, got, "header %q", header)
	}
}
