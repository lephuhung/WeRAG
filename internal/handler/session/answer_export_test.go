package session

import (
	"archive/zip"
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/Tencent/WeKnora/internal/application/service"
	apperrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/middleware"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

const exportTenant uint64 = 10000

// exportSessions holds sessions by id with their owner; Get honours the
// per-user scope like the real repository.
type exportSessions struct {
	interfaces.SessionRepository
	owners map[string]string
}

func (r *exportSessions) Get(_ context.Context, tenantID uint64, ownerID, id string) (*types.Session, error) {
	owner, ok := r.owners[id]
	if !ok || tenantID != exportTenant || (ownerID != "" && owner != ownerID) {
		return nil, apperrors.ErrSessionNotFound
	}
	return &types.Session{ID: id, TenantID: tenantID, UserID: owner}, nil
}

func (r *exportSessions) GetIMPlatform(context.Context, uint64, string) (string, error) {
	return "", nil
}

type exportMessages struct {
	interfaces.MessageRepository
	msgs []*types.Message
}

func (r *exportMessages) GetMessage(_ context.Context, sessionID, id string) (*types.Message, error) {
	for _, m := range r.msgs {
		if m.ID == id && m.SessionID == sessionID {
			cp := *m
			return &cp, nil
		}
	}
	return nil, gorm.ErrRecordNotFound
}

func (r *exportMessages) GetMessagesByRequestIDs(_ context.Context, sessionID string, ids []string) ([]*types.MessageWithSession, error) {
	var out []*types.MessageWithSession
	for _, m := range r.msgs {
		if m.SessionID == sessionID && len(ids) == 1 && m.RequestID == ids[0] {
			out = append(out, &types.MessageWithSession{Message: *m})
		}
	}
	return out, nil
}

type exportKBs struct {
	interfaces.KnowledgeBaseService
}

func (exportKBs) GetKnowledgeBasesByIDsOnly(_ context.Context, ids []string) ([]*types.KnowledgeBase, error) {
	var out []*types.KnowledgeBase
	for _, id := range ids {
		if id == "kb-legal" {
			out = append(out, &types.KnowledgeBase{ID: id, Name: "Văn bản pháp luật"})
		}
	}
	return out, nil
}

type exportReviews struct {
	interfaces.LegalReviewRepository
}

func (exportReviews) ApprovedLinksForKnowledge(_ context.Context, ids []string) (map[string]*types.LegalDocumentLink, error) {
	out := map[string]*types.LegalDocumentLink{}
	for _, id := range ids {
		if id == "k-nd30" {
			out[id] = &types.LegalDocumentLink{KnowledgeID: id, LegalDocumentID: "vbpl-1"}
		}
	}
	return out, nil
}

type exportCatalog struct {
	interfaces.LegalCatalogRepository
}

func (exportCatalog) GetByIDs(_ context.Context, ids []string) ([]*types.LegalDocument, error) {
	return []*types.LegalDocument{{ID: "vbpl-1", DocumentNumber: "30/2020/NĐ-CP"}}, nil
}

func newExportEngine(t *testing.T, userID string) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	at := time.Date(2026, 10, 16, 7, 5, 0, 0, time.UTC) // 14h05 in Vietnam
	msgs := &exportMessages{msgs: []*types.Message{
		{ID: "q1", SessionID: "s-alice", RequestID: "r1", Role: "user", Content: "Hồ sơ gồm gì?", IsCompleted: true, CreatedAt: at.Add(-time.Minute)},
		{ID: "a1", SessionID: "s-alice", RequestID: "r1", Role: "assistant", IsCompleted: true, CreatedAt: at,
			Content: "Theo **Điều 8**:\n\n- Đơn\n- Bản sao",
			KnowledgeReferences: types.References{
				{ID: "c1", KnowledgeID: "k-nd30", KnowledgeBaseID: "kb-legal", KnowledgeTitle: "Nghị định công tác văn thư"},
				{ID: "c2", KnowledgeID: "k-nd30", KnowledgeBaseID: "kb-legal", KnowledgeTitle: "Nghị định công tác văn thư"},
				{ID: "https://chinhphu.vn/x", KnowledgeID: "https://chinhphu.vn/x", KnowledgeTitle: "Cổng Chính phủ",
					ChunkType: string(types.ChunkTypeWebSearch), Metadata: map[string]string{"url": "https://chinhphu.vn/x"}},
			}},
		{ID: "a2", SessionID: "s-alice", RequestID: "r2", Role: "assistant", IsCompleted: true, CreatedAt: at,
			ExecutionContext: types.MessageExecutionContext{TurnFailure: types.TurnFailureModelInterrupted}, Content: "Một phần"},
		{ID: "a3", SessionID: "s-alice", RequestID: "r3", Role: "assistant", IsCompleted: false, CreatedAt: at, Content: "Đang viết"},
		{ID: "b1", SessionID: "s-bob", RequestID: "r9", Role: "assistant", IsCompleted: true, CreatedAt: at, Content: "Của Bob"},
	}}
	svc := service.NewAnswerExportService(msgs, &exportSessions{owners: map[string]string{"s-alice": "alice", "s-bob": "bob"}},
		exportKBs{}, nil, nil, exportReviews{}, exportCatalog{})
	h := NewAnswerExportHandler(svc)
	r := gin.New()
	r.Use(middleware.ErrorHandler())
	r.Use(func(c *gin.Context) {
		ctx := context.WithValue(c.Request.Context(), types.TenantIDContextKey, exportTenant)
		ctx = context.WithValue(ctx, types.UserIDContextKey, userID)
		c.Request = c.Request.WithContext(ctx)
		c.Next()
	})
	r.GET("/sessions/:id/messages/:message_id/export.docx", h.ExportAnswerDocx)
	return r
}

func getExport(r *gin.Engine, session, message string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/sessions/"+session+"/messages/"+message+"/export.docx", nil))
	return rec
}

func TestExportAnswerDocxWritesTheAnswer(t *testing.T) {
	rec := getExport(newExportEngine(t, "alice"), "s-alice", "a1")
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d body=%s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != docxContentType {
		t.Errorf("content type %q", ct)
	}
	cd := rec.Header().Get("Content-Disposition")
	if !strings.Contains(cd, `filename="Tra loi - Ho so gom gi.docx"`) ||
		!strings.Contains(cd, `filename*=UTF-8''Tr%E1%BA%A3%20l%E1%BB%9Di%20-%20H%E1%BB%93%20s%C6%A1%20g%E1%BB%93m%20g%C3%AC.docx`) {
		t.Errorf("Content-Disposition = %s", cd)
	}
	data := rec.Body.Bytes()
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("not a docx: %v", err)
	}
	var doc string
	for _, f := range zr.File {
		if f.Name == "word/document.xml" {
			rc, _ := f.Open()
			b, _ := io.ReadAll(rc)
			rc.Close()
			doc = string(b)
		}
	}
	for _, want := range []string{
		">Hồ sơ gồm gì?</w:t>", ">14h05 - 16/10/2026 · Trả lời bởi WeRAG</w:t>", ">Điều 8</w:t>", ">Bản sao</w:t>",
		">Tài liệu tham khảo</w:t>", ">Nghị định công tác văn thư</w:t>", "> — Số hiệu: 30/2020/NĐ-CP</w:t>",
		"> — Văn bản pháp luật</w:t>", ">Cổng Chính phủ</w:t>", "> — https://chinhphu.vn/x</w:t>",
	} {
		if !strings.Contains(doc, want) {
			t.Errorf("document.xml lacks %s", want)
		}
	}
	// two chunks of one document are one source
	if n := strings.Count(doc, ">Nghị định công tác văn thư</w:t>"); n != 1 {
		t.Errorf("document listed %d times", n)
	}
}

func TestExportAnswerDocxRefusals(t *testing.T) {
	cases := []struct {
		name, user, session, message string
		status                       int
		msg                          string
	}{
		{"another user's session", "alice", "s-bob", "b1", http.StatusNotFound, ""},
		{"another user's message through own session", "alice", "s-alice", "b1", http.StatusNotFound, ""},
		{"owner cannot be impersonated", "bob", "s-alice", "a1", http.StatusNotFound, ""},
		{"unknown message", "alice", "s-alice", "nope", http.StatusNotFound, ""},
		{"user message", "alice", "s-alice", "q1", http.StatusBadRequest, apperrors.ErrAnswerExportNotAnswer.Error()},
		{"failed turn", "alice", "s-alice", "a2", http.StatusBadRequest, apperrors.ErrAnswerExportFailedTurn.Error()},
		{"still writing", "alice", "s-alice", "a3", http.StatusBadRequest, apperrors.ErrAnswerExportIncomplete.Error()},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := getExport(newExportEngine(t, tc.user), tc.session, tc.message)
			if rec.Code != tc.status {
				t.Fatalf("status %d, want %d, body=%s", rec.Code, tc.status, rec.Body.String())
			}
			if rec.Header().Get("Content-Disposition") != "" {
				t.Error("refusal carries a file")
			}
			if tc.msg != "" && !strings.Contains(rec.Body.String(), tc.msg) {
				t.Errorf("body %s lacks %q", rec.Body.String(), tc.msg)
			}
		})
	}
}

// A client that left gets no file and no error log: 499, as nginx writes it.
func TestExportAnswerDocxClientGone(t *testing.T) {
	r := newExportEngine(t, "alice")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/sessions/s-alice/messages/a1/export.docx", nil).WithContext(ctx)
	r.ServeHTTP(rec, req)
	if rec.Code != 499 {
		t.Fatalf("status %d, want 499, body=%s", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("Content-Disposition") != "" || rec.Body.Len() != 0 {
		t.Errorf("a file went out: %q", rec.Body.String())
	}
}
