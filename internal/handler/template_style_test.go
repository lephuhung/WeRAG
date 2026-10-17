package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/Tencent/WeKnora/internal/middleware"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

type memTemplateStyles struct {
	rows map[uint64]*types.TenantTemplateStyle
}

func (m *memTemplateStyles) Get(_ context.Context, tenantID uint64) (*types.TenantTemplateStyle, error) {
	return m.rows[tenantID], nil
}

func (m *memTemplateStyles) Save(_ context.Context, s *types.TenantTemplateStyle) error {
	m.rows[s.TenantID] = s
	return nil
}

type templateStyleSettings struct {
	interfaces.SystemSettingService
	raw string
}

func (s *templateStyleSettings) GetJSON(_ context.Context, key string) (types.JSON, bool) {
	if key != types.DocumentTemplateFontSizesSettingKey || s.raw == "" {
		return nil, false
	}
	return types.JSON(s.raw), true
}

func newTemplateStyleRouter(repo *memTemplateStyles, platform string) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(middleware.ErrorHandler())
	r.Use(func(c *gin.Context) {
		c.Set(types.TenantIDContextKey.String(), uint64(7))
		c.Request = c.Request.WithContext(context.WithValue(c.Request.Context(), types.UserIDContextKey, "u-admin"))
		c.Next()
	})
	h := NewTemplateStyleHandler(repo, &templateStyleSettings{raw: platform})
	r.GET("/tenant/document-template-style", h.GetTemplateStyle)
	r.PUT("/tenant/document-template-style", h.UpdateTemplateStyle)
	return r
}

func decodeTemplateStyle(t *testing.T, w *httptest.ResponseRecorder) TemplateStyleResponse {
	t.Helper()
	var body struct {
		Data TemplateStyleResponse `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode %s: %v", w.Body.String(), err)
	}
	return body.Data
}

func TestTemplateStyleGetAndSave(t *testing.T) {
	repo := &memTemplateStyles{rows: map[uint64]*types.TenantTemplateStyle{}}
	r := newTemplateStyleRouter(repo, `{"quoc_hieu":12}`)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/tenant/document-template-style", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("get: %d %s", w.Code, w.Body.String())
	}
	got := decodeTemplateStyle(t, w)
	if len(got.Components) != len(types.TemplateFontSizeSpecs) || got.Platform["quoc_hieu"] != 12 ||
		len(got.Tenant) != 0 || got.Effective["quoc_hieu"] != 12 || got.Effective["tieu_ngu"] != 14 {
		t.Fatalf("get: %+v", got)
	}

	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPut, "/tenant/document-template-style",
		strings.NewReader(`{"font_sizes":{"quoc_hieu":13,"le_trai":35}}`)))
	if w.Code != http.StatusOK {
		t.Fatalf("put: %d %s", w.Code, w.Body.String())
	}
	got = decodeTemplateStyle(t, w)
	if got.Tenant["quoc_hieu"] != 13 || got.Effective["quoc_hieu"] != 13 || got.Effective["le_trai"] != 35 {
		t.Fatalf("put: %+v", got)
	}
	if row := repo.rows[7]; row == nil || row.UpdatedBy != "u-admin" {
		t.Fatalf("saved row: %+v", row)
	}

	// an empty save clears the override
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPut, "/tenant/document-template-style", strings.NewReader(`{}`)))
	if got := decodeTemplateStyle(t, w); w.Code != http.StatusOK || len(got.Tenant) != 0 || got.Effective["quoc_hieu"] != 12 {
		t.Fatalf("clear: %d %+v", w.Code, got)
	}
}

func TestTemplateStyleRejectsOutOfRange(t *testing.T) {
	repo := &memTemplateStyles{rows: map[uint64]*types.TenantTemplateStyle{}}
	r := newTemplateStyleRouter(repo, "")
	for _, body := range []string{
		`{"font_sizes":{"tieu_ngu":12}}`,
		`{"font_sizes":{"so_ky_hieu":13}}`,
		`{"font_sizes":{"le_phai":"15"}}`,
		`not json`,
	} {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodPut, "/tenant/document-template-style", strings.NewReader(body)))
		if w.Code != http.StatusBadRequest {
			t.Errorf("%s: want 400, got %d %s", body, w.Code, w.Body.String())
		}
	}
	if len(repo.rows) != 0 {
		t.Fatalf("an invalid save was stored: %+v", repo.rows)
	}
}

// A stored row a later-narrowed range no longer allows follows the platform
// instead of failing the editor.
func TestTemplateStyleUnreadableRowFollowsPlatform(t *testing.T) {
	repo := &memTemplateStyles{rows: map[uint64]*types.TenantTemplateStyle{
		7: {TenantID: 7, FontSizes: types.JSON(`{"quoc_hieu":99}`)},
	}}
	w := httptest.NewRecorder()
	newTemplateStyleRouter(repo, "").ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/tenant/document-template-style", nil))
	got := decodeTemplateStyle(t, w)
	if w.Code != http.StatusOK || len(got.Tenant) != 0 || got.Effective["quoc_hieu"] != 13 {
		t.Fatalf("got %d %+v", w.Code, got)
	}
}
