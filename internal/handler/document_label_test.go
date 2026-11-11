package handler

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/middleware"
	"github.com/Tencent/WeKnora/internal/types"
)

type labelCaller struct {
	tenantID   uint64
	role       types.TenantRole
	superAdmin bool
	apiKey     bool
}

type labelTenants map[uint64]string

func (m labelTenants) GetTenantsByIDs(_ context.Context, ids []uint64) (map[uint64]*types.Tenant, error) {
	out := map[uint64]*types.Tenant{}
	for _, id := range ids {
		if name, ok := m[id]; ok {
			out[id] = &types.Tenant{ID: id, Name: name}
		}
	}
	return out, nil
}

func newLabelRouter(t *testing.T) (*gin.Engine, *gorm.DB, *labelCaller) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&types.DocumentSpellingFeedback{}, &types.DocumentFormatFeedback{}))
	h := &DocumentLabelHandler{
		repo:    repository.NewDocumentLabelRepository(db),
		tenants: labelTenants{1: "Sở Tư pháp", 2: "UBND xã"},
	}
	who := &labelCaller{}
	r := gin.New()
	r.Use(middleware.ErrorHandler(), func(c *gin.Context) {
		ctx := types.WithCaller(c.Request.Context(), types.Caller{TenantID: who.tenantID, UserID: "u-admin", Role: who.role})
		ctx = context.WithValue(ctx, types.UserIDContextKey, "u-admin")
		ctx = context.WithValue(ctx, types.TenantIDContextKey, who.tenantID)
		if who.superAdmin {
			ctx = context.WithValue(ctx, types.SystemAdminContextKey, true)
		}
		if who.apiKey {
			ctx = types.WithTenantAPIKeyScope(ctx, types.TenantAPIKeyScope{KeyID: 1, FullAccess: true})
		}
		c.Request = c.Request.WithContext(ctx)
		c.Next()
	})
	g := r.Group("/api/v1/document-labels")
	g.GET("", h.ListDocumentLabels)
	g.GET("/facets", h.DocumentLabelFacets)
	g.GET("/export", h.ExportDocumentLabels)
	return r, db, who
}

func seedLabels(t *testing.T, db *gorm.DB) {
	t.Helper()
	at := func(day int) time.Time { return time.Date(2026, 11, day, 10, 0, 0, 0, time.Local) }
	spelling := []types.DocumentSpellingFeedback{
		{TenantID: 1, UserID: "u1", Paragraph: "Căn cứ Luật Tổ chức chính quyền địa phương, chính quyền", ParagraphHash: "h1",
			Wrong: "chính quyền", Suggestion: "chính quyền", TextOccurrence: 2, Verdict: types.SpellingVerdictNotError,
			Source: types.SpellingFeedbackSourceUser, Model: "spellcheck-gemma4-e4b-v4", CreatedAt: at(3)},
		{TenantID: 1, UserID: "u1", Paragraph: "Kính gửi: Ủy ban nhân dân", ParagraphHash: "h2",
			Wrong: "Ủy", Suggestion: "Uỷ", TextOccurrence: 1, Verdict: types.SpellingVerdictNotError,
			Source: types.SpellingFeedbackSourceVerifier, Model: "spellcheck-gemma4-e4b-v4", Note: "verifier qwen: đúng", CreatedAt: at(5)},
		{TenantID: 2, UserID: "u2", Paragraph: "Tổ chức khác", ParagraphHash: "h3",
			Wrong: "khác", Suggestion: "khac", TextOccurrence: 1, Verdict: types.SpellingVerdictNotError,
			Source: types.SpellingFeedbackSourceUser, Model: "other-model", CreatedAt: at(4)},
	}
	require.NoError(t, db.Create(&spelling).Error)
	report, err := json.Marshal(types.DocumentFormatReport{Findings: []types.DocumentFormatFinding{{
		ID: "font_size#1", CheckID: "font_size", Title: "Cỡ chữ nội dung", Current: "12pt", Expected: "13–14pt",
		Anchor: &types.DocumentFormatAnchor{Text: "Điều 1. Phạm vi điều chỉnh", Occurrence: 1},
		Text:   "Phạm vi", TextOccurrence: 1,
	}}})
	require.NoError(t, err)
	format := []types.DocumentFormatFeedback{
		{TenantID: 1, ReportID: "fc-1", FindingID: "font_size#1", CheckID: "font_size", FindingTitle: "Cỡ chữ nội dung",
			Verdict: types.FormatVerdictFalseAlarm, Model: "qwen3", DocumentType: "cong_van", Scope: "full",
			Report: report, CreatedAt: at(6)},
		{TenantID: 1, ReportID: "fc-1", Verdict: types.FormatVerdictNotND30, Note: "mẫu tố tụng",
			Report: report, CreatedAt: at(1)},
	}
	require.NoError(t, db.Create(&format).Error)
}

func labelGet(t *testing.T, r *gin.Engine, path string) (int, map[string]any) {
	t.Helper()
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
	var body map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	return w.Code, body
}

func labelItems(t *testing.T, body map[string]any) []map[string]any {
	t.Helper()
	data := body["data"].(map[string]any)
	var out []map[string]any
	for _, it := range data["items"].([]any) {
		out = append(out, it.(map[string]any))
	}
	return out
}

func TestDocumentLabelsTenantAdminSeesOwnOrganization(t *testing.T) {
	r, db, who := newLabelRouter(t)
	seedLabels(t, db)
	who.tenantID, who.role = 1, types.TenantRoleAdmin

	code, body := labelGet(t, r, "/api/v1/document-labels")
	require.Equal(t, http.StatusOK, code, body)
	items := labelItems(t, body)
	require.EqualValues(t, 4, body["data"].(map[string]any)["total"])
	var ids []string
	for _, it := range items {
		require.EqualValues(t, 1, it["organization_id"])
		require.Equal(t, "Sở Tư pháp", it["organization_name"])
		require.NotContains(t, it, "user_id", "labels carry no user identity")
		ids = append(ids, it["kind"].(string))
	}
	require.Equal(t, []string{"format", "spelling", "spelling", "format"}, ids, "newest first across both tables")

	// the format finding resolved from the kept report
	f := items[0]
	require.Equal(t, "Điều 1. Phạm vi điều chỉnh", f["paragraph"])
	require.Equal(t, "Phạm vi", f["flagged_text"])
	require.EqualValues(t, 8, f["span_start"])
	require.EqualValues(t, 15, f["span_end"])
	require.Equal(t, "13–14pt", f["suggestion"])
	require.Equal(t, "font_size", f["check_id"])
	// the second instance of the flagged text (rune offsets)
	s := items[2]
	require.Equal(t, "chính quyền", s["flagged_text"])
	require.EqualValues(t, 44, s["span_start"])
	require.EqualValues(t, 55, s["span_end"])
	// a verdict on the whole report has no paragraph
	require.Equal(t, "", items[3]["paragraph"])
	require.Nil(t, items[3]["span_start"])

	// another organization is refused, not silently widened
	code, _ = labelGet(t, r, "/api/v1/document-labels?tenant_id=2")
	require.Equal(t, http.StatusForbidden, code)
	code, _ = labelGet(t, r, "/api/v1/document-labels?tenant_id=1&kind=spelling&source=verifier")
	require.Equal(t, http.StatusOK, code)

	// filters
	_, body = labelGet(t, r, "/api/v1/document-labels?kind=spelling&source=verifier")
	require.Len(t, labelItems(t, body), 1)
	_, body = labelGet(t, r, "/api/v1/document-labels?verdict=false_alarm")
	require.Len(t, labelItems(t, body), 1)
	_, body = labelGet(t, r, "/api/v1/document-labels?from=2026-11-03&to=2026-11-05")
	require.Len(t, labelItems(t, body), 2, "to is inclusive")
	_, body = labelGet(t, r, "/api/v1/document-labels?model=qwen3")
	require.Len(t, labelItems(t, body), 1)
	_, body = labelGet(t, r, "/api/v1/document-labels?page=2&page_size=3")
	require.Len(t, labelItems(t, body), 1)
	code, _ = labelGet(t, r, "/api/v1/document-labels?page=41&page_size=50")
	require.Equal(t, http.StatusBadRequest, code, "past 2,000 rows: narrow the filters or export")
	code, _ = labelGet(t, r, "/api/v1/document-labels?kind=other")
	require.Equal(t, http.StatusBadRequest, code)
	code, _ = labelGet(t, r, "/api/v1/document-labels?from=03/11/2026")
	require.Equal(t, http.StatusBadRequest, code)

	_, body = labelGet(t, r, "/api/v1/document-labels/facets")
	require.Equal(t, []any{"qwen3", "spellcheck-gemma4-e4b-v4"}, body["data"].(map[string]any)["models"])
}

func TestDocumentLabelsSuperAdminFilter(t *testing.T) {
	r, db, who := newLabelRouter(t)
	seedLabels(t, db)
	who.tenantID, who.superAdmin = 1, true // no tenant role needed

	_, body := labelGet(t, r, "/api/v1/document-labels")
	require.EqualValues(t, 5, body["data"].(map[string]any)["total"], "every organization")
	_, body = labelGet(t, r, "/api/v1/document-labels?tenant_id=2")
	items := labelItems(t, body)
	require.Len(t, items, 1)
	require.Equal(t, "UBND xã", items[0]["organization_name"])
	_, body = labelGet(t, r, "/api/v1/document-labels/facets?tenant_id=2")
	require.Equal(t, []any{"other-model"}, body["data"].(map[string]any)["models"])
}

func TestDocumentLabelsRefused(t *testing.T) {
	r, db, who := newLabelRouter(t)
	seedLabels(t, db)
	for name, c := range map[string]labelCaller{
		"member":             {tenantID: 1, role: types.TenantRoleMember},
		"no role":            {tenantID: 1},
		"API key":            {tenantID: 1, role: types.TenantRoleAdmin, apiKey: true},
		"SuperAdmin API key": {tenantID: 1, superAdmin: true, apiKey: true},
	} {
		*who = c
		for _, p := range []string{"/api/v1/document-labels", "/api/v1/document-labels/export", "/api/v1/document-labels/facets"} {
			code, _ := labelGet(t, r, p)
			require.Equal(t, http.StatusForbidden, code, "%s %s", name, p)
		}
	}
}

func TestDocumentLabelsExportJSONL(t *testing.T) {
	r, db, who := newLabelRouter(t)
	seedLabels(t, db)
	who.tenantID, who.role = 1, types.TenantRoleAdmin

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/document-labels/export", nil))
	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Header().Get("Content-Type"), "application/x-ndjson")
	require.Contains(t, w.Header().Get("Content-Disposition"), ".jsonl")

	var lines []map[string]any
	sc := bufio.NewScanner(strings.NewReader(w.Body.String()))
	for sc.Scan() {
		var rec map[string]any
		require.NoError(t, json.Unmarshal(sc.Bytes(), &rec), sc.Text())
		lines = append(lines, rec)
	}
	require.Len(t, lines, 5, "one line per label of the organization, then the trailer")
	require.Equal(t, map[string]any{"_end": true, "rows": float64(4)}, lines[4], "a complete file ends with the trailer")
	lines = lines[:4]
	for _, rec := range lines {
		for _, key := range []string{"id", "kind", "organization_id", "created_at", "paragraph", "span_start", "span_end",
			"flagged_text", "suggestion", "verdict", "source", "check_id", "model"} {
			require.Contains(t, rec, key)
		}
		require.EqualValues(t, 1, rec["organization_id"])
		_, err := time.Parse(time.RFC3339, rec["created_at"].(string))
		require.NoError(t, err)
	}
	require.Equal(t, "spelling", lines[0]["kind"], "spelling first, oldest first")
	require.Equal(t, "Căn cứ Luật Tổ chức chính quyền địa phương, chính quyền", lines[0]["paragraph"])
	require.Equal(t, "verifier", lines[1]["source"])
	require.Equal(t, "format", lines[2]["kind"])
	require.True(t, strings.HasPrefix(lines[2]["id"].(string), "format:"))
	// HTML is not escaped in the file
	require.NotContains(t, w.Body.String(), `<`)

	// filters apply to the export too
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/document-labels/export?kind=format", nil))
	require.Equal(t, 3, strings.Count(w.Body.String(), "\n"), "2 labels + trailer")
}

// Labels at the same instant keep a stable order: spelling before format,
// then the higher (numeric) id first, so pages never overlap.
func TestDocumentLabelsTieOrder(t *testing.T) {
	r, db, who := newLabelRouter(t)
	who.tenantID, who.role = 1, types.TenantRoleAdmin
	at := time.Date(2026, 11, 2, 8, 0, 0, 0, time.Local)
	for i := 0; i < 11; i++ {
		require.NoError(t, db.Create(&types.DocumentSpellingFeedback{TenantID: 1, Paragraph: "p", ParagraphHash: "h",
			Wrong: "p", Verdict: types.SpellingVerdictNotError, Source: types.SpellingFeedbackSourceUser, CreatedAt: at}).Error)
	}
	require.NoError(t, db.Create(&types.DocumentFormatFeedback{TenantID: 1, ReportID: "r", Verdict: types.FormatVerdictOther, CreatedAt: at}).Error)
	var ids []string
	for page := 1; page <= 3; page++ {
		_, body := labelGet(t, r, "/api/v1/document-labels?page_size=5&page="+strconv.Itoa(page))
		for _, it := range labelItems(t, body) {
			ids = append(ids, it["id"].(string))
		}
	}
	require.Equal(t, []string{"spelling:11", "spelling:10", "spelling:9", "spelling:8", "spelling:7", "spelling:6",
		"spelling:5", "spelling:4", "spelling:3", "spelling:2", "spelling:1", "format:1"}, ids)
}
