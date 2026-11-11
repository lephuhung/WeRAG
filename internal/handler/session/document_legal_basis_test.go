package session

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/application/service"
	"github.com/Tencent/WeKnora/internal/docformat/docxwrite"
	"github.com/Tencent/WeKnora/internal/middleware"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/Tencent/WeKnora/internal/vietnamese_legal"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// legalBasisWorkspaces serves one target document with a stored file.
type legalBasisWorkspaces struct {
	routeWorkspaces
	content   []byte
	snapshots []string
	saveErr   error
	notSaved  bool
}

func (f *legalBasisWorkspaces) OpenCurrent(ctx context.Context, tenantID uint64, sessionID, documentID string) (io.ReadCloser, *types.DocumentWorkspace, error) {
	ws, err := f.Get(ctx, tenantID, sessionID, documentID)
	if err != nil {
		return nil, nil, err
	}
	return io.NopCloser(bytes.NewReader(f.content)), ws, nil
}

func (f *legalBasisWorkspaces) Snapshot(_ context.Context, _ uint64, _, _, label, source string, _ time.Duration) (*types.DocumentRevision, error) {
	f.snapshots = append(f.snapshots, source+"|"+label)
	return &types.DocumentRevision{Seq: 1}, f.saveErr
}

// SnapshotSaved is the real service's variant that reports whether the
// editor's save landed (a failed or timed-out force-save is no error).
func (f *legalBasisWorkspaces) SnapshotSaved(ctx context.Context, tenantID uint64, sessionID, documentID, label, source string, wait time.Duration) (*types.DocumentRevision, bool, error) {
	rev, err := f.Snapshot(ctx, tenantID, sessionID, documentID, label, source, wait)
	return rev, err == nil && !f.notSaved, err
}

// legalBasisCatalog answers number lookups from rows; the rest of the
// repository is not used by the check.
type legalBasisCatalog struct {
	interfaces.LegalCatalogRepository
	docs []*types.LegalDocument
}

func (c *legalBasisCatalog) FindByNumberKey(_ context.Context, key string, _ int) ([]*types.LegalDocument, error) {
	var out []*types.LegalDocument
	for _, d := range c.docs {
		if d.NumberKey == key {
			out = append(out, d)
		}
	}
	return out, nil
}

func (c *legalBasisCatalog) FindByNumberPrefix(context.Context, string, int) ([]*types.LegalDocument, error) {
	return nil, nil
}

func (c *legalBasisCatalog) ListLinks(context.Context, string, []types.LegalRelation, []types.LegalRelation) ([]types.LegalRelationLink, error) {
	return nil, nil
}

func TestCheckDocumentLegalBasisRoute(t *testing.T) {
	content, err := docxwrite.Build(docxwrite.Document{Paragraphs: []docxwrite.Paragraph{
		{Text: "Căn cứ Thông tư 01/2011/TT-BNV ngày 19/01/2011;"},
		{Text: "Căn cứ Nghị định 99/2025/NĐ-CP."},
	}})
	require.NoError(t, err)
	ws := &legalBasisWorkspaces{
		routeWorkspaces: routeWorkspaces{enabled: true, bySess: map[string]*types.DocumentWorkspace{
			"mine": {ID: "ws-mine", SessionID: "mine", TenantID: 7, Revision: 4, Status: types.DocumentWorkspaceStatusOpen, FileName: "qd.docx"},
		}},
		content: content,
	}
	precheck := service.NewDocumentFormatPrecheck(ws, nil, nil, nil)
	h := NewDocumentWorkspaceHandler(&ownerOnlySessions{owned: map[string]bool{"mine": true, "admin-view": false}}, ws, nil, precheck)
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(middleware.ErrorHandler())
	r.Use(func(c *gin.Context) { c.Set(types.TenantIDContextKey.String(), uint64(7)) })
	r.POST("/sessions/:session_id/documents/:doc_id/legal-basis-check", h.CheckDocumentLegalBasis)

	// no catalogue wired: unavailable, not an empty "all clear"
	rec := doJSON(r, http.MethodPost, "/sessions/mine/documents/ws-mine/legal-basis-check", "", nil)
	require.Equal(t, http.StatusServiceUnavailable, rec.Code, rec.Body.String())

	expired := &types.LegalDocument{ID: "tt01", DocumentNumber: "01/2011/TT-BNV", NumberKey: vietnamese_legal.CatalogNumberKey("01/2011/TT-BNV"),
		DocType: "Thông tư", Status: "Hết hiệu lực toàn bộ"}
	precheck.SetLegalBasis(&legalBasisCatalog{docs: []*types.LegalDocument{expired}}, nil)

	// only the owner may run it
	rec = doJSON(r, http.MethodPost, "/sessions/admin-view/documents/ws-mine/legal-basis-check", "", nil)
	require.Equal(t, http.StatusNotFound, rec.Code)
	rec = doJSON(r, http.MethodPost, "/sessions/mine/documents/other/legal-basis-check", "", nil)
	require.Equal(t, http.StatusNotFound, rec.Code)

	rec = doJSON(r, http.MethodPost, "/sessions/mine/documents/ws-mine/legal-basis-check", "", nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Equal(t, []string{types.DocumentRevisionSourceAI + "|ai: rà soát căn cứ pháp lý"}, ws.snapshots, "the editor is saved first")
	var got struct {
		Success bool `json:"success"`
		Data    struct {
			DocumentID string `json:"document_id"`
			Revision   int    `json:"revision"`
			Unsaved    bool   `json:"unsaved"`
			Findings   []struct {
				Citation  string `json:"citation"`
				Status    string `json:"status"`
				Note      string `json:"note"`
				Positions []struct {
					Paragraph      int    `json:"paragraph"`
					Text           string `json:"text"`
					TextOccurrence int    `json:"text_occurrence"`
					Anchor         struct {
						Text string `json:"text"`
					} `json:"anchor"`
				} `json:"positions"`
			} `json:"findings"`
			Summary     map[string]int   `json:"summary"`
			DocumentOps []map[string]any `json:"document_ops"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	require.True(t, got.Success)
	require.Equal(t, "ws-mine", got.Data.DocumentID)
	require.Equal(t, 4, got.Data.Revision)
	require.False(t, got.Data.Unsaved)
	require.Len(t, got.Data.Findings, 2)
	require.Equal(t, "01/2011/TT-BNV", got.Data.Findings[0].Citation)
	require.Equal(t, "expired", got.Data.Findings[0].Status)
	require.Equal(t, "not_found", got.Data.Findings[1].Status)
	require.NotContains(t, got.Data.Findings[1].Note, "hết hiệu lực")
	p := got.Data.Findings[0].Positions[0]
	require.Equal(t, 0, p.Paragraph)
	require.Equal(t, "01/2011/TT-BNV", p.Text)
	require.Equal(t, 1, p.TextOccurrence)
	require.True(t, strings.HasPrefix(p.Anchor.Text, "Căn cứ Thông tư"))
	require.Equal(t, map[string]int{"expired": 1, "not_found": 1}, got.Data.Summary)
	for _, op := range got.Data.DocumentOps {
		require.Equal(t, "mark", op["op"])
		require.Equal(t, "underline", op["style"])
	}

	// only the questioned statuses are underlined by default: the expired
	// one, not the one missing from the catalogue
	require.Len(t, got.Data.DocumentOps, 1)

	// a save that did not land (force-save failed or timed out, no error)
	// checks the stored version and says so
	ws.notSaved = true
	rec = doJSON(r, http.MethodPost, "/sessions/mine/documents/ws-mine/legal-basis-check", "", nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	got.Data.Unsaved = false
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	require.True(t, got.Data.Unsaved)
	ws.notSaved = false
	ws.saveErr = errors.New("document server down")
	rec = doJSON(r, http.MethodPost, "/sessions/mine/documents/ws-mine/legal-basis-check", "", nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	got.Data.Unsaved = false
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	require.True(t, got.Data.Unsaved)

	// a closed editor is not checked (the format check's rule)
	ws.saveErr = nil
	ws.bySess["mine"].Status = types.DocumentWorkspaceStatusClosed
	rec = doJSON(r, http.MethodPost, "/sessions/mine/documents/ws-mine/legal-basis-check", "", nil)
	require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
	ws.bySess["mine"].Status = types.DocumentWorkspaceStatusOpen

	// a source document is not checked
	ws.bySess["mine"].Role = types.DocumentWorkspaceRoleSource
	rec = doJSON(r, http.MethodPost, "/sessions/mine/documents/ws-mine/legal-basis-check", "", nil)
	require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
}
