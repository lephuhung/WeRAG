package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Tencent/WeKnora/internal/application/service"
	"github.com/gin-gonic/gin"
)

// uiThemeTestSettings reuses the no-op runtime settings fake and overrides
// GetString so the resolver returns a fixed theme (empty → built-in default).
type uiThemeTestSettings struct {
	runtimeTestSettings
	theme string
}

func (s uiThemeTestSettings) GetString(_ context.Context, key, _ string, def string) string {
	if key == service.SystemUIThemeKey && s.theme != "" {
		return s.theme
	}
	return def
}

func TestGetSystemUITheme(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tests := []struct {
		name  string
		theme string
		want  string
	}{
		{name: "anthropic", theme: "anthropic", want: "anthropic"},
		{name: "empty falls back to default", theme: "", want: "elevenlabs"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := &SystemHandler{systemSettingSvc: uiThemeTestSettings{theme: tt.theme}}
			engine := gin.New()
			engine.GET("/api/v1/system/ui-theme", h.GetSystemUITheme)
			w := httptest.NewRecorder()
			engine.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/system/ui-theme", nil))
			if w.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200, body=%s", w.Code, w.Body.String())
			}
			var body struct {
				Success bool `json:"success"`
				Data    struct {
					Theme string `json:"theme"`
				} `json:"data"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
				t.Fatalf("decode: %v", err)
			}
			if !body.Success || body.Data.Theme != tt.want {
				t.Fatalf("body = %s, want theme=%q", w.Body.String(), tt.want)
			}
		})
	}
}
