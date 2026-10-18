package middleware

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
)

// A secret setting is saved as {"value": "<secret>"}: the field name says
// nothing, so the handler flags the request and the logger drops the body.
func TestLoggerOmitsFlaggedRequestBody(t *testing.T) {
	var out bytes.Buffer
	logger.SetOutput(&out)
	defer logger.SetOutput(os.Stdout)

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(Logger())
	r.PUT("/s/:key", func(c *gin.Context) {
		if c.Param("key") == "secret" {
			c.Set(types.LogOmitRequestBodyContextKey.String(), true)
		}
		c.Status(http.StatusOK)
	})
	put := func(key, body string) {
		req := httptest.NewRequest(http.MethodPut, "/s/"+key, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(httptest.NewRecorder(), req)
	}

	put("secret", `{"value":"S3CRET-0123456789abcdef0123456789abcdef"}`)
	put("plain", `{"value":"visible-value"}`)
	if strings.Contains(out.String(), "S3CRET") {
		t.Fatalf("flagged request body was logged:\n%s", out.String())
	}
	if !strings.Contains(out.String(), "visible-value") {
		t.Fatalf("other request bodies should still be logged:\n%s", out.String())
	}
}
