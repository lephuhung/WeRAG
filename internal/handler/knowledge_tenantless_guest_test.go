package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// TestKnowledgeResolveTenantlessGuest: a tenantless authenticated human
// (Guest) reads and downloads documents of a public/published KB through
// the handler-side resolution, while tenant KBs and every Editor-level
// resolution stay denied.
func TestKnowledgeResolveTenantlessGuest(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tt := range []struct {
		name     string
		kb       *types.KnowledgeBase
		readOK   bool
		downOK   bool
		editorOK bool
	}{
		{"public", &types.KnowledgeBase{ID: "kb", TenantID: 9, Visibility: types.KBVisibilityPublic}, true, true, false},
		{"published", &types.KnowledgeBase{ID: "kb", TenantID: 5, OwnerTenantID: 5, Visibility: types.KBVisibilityPublished}, true, true, false},
		{"tenant", &types.KnowledgeBase{ID: "kb", TenantID: 5, OwnerTenantID: 5, Visibility: types.KBVisibilityTenant}, false, false, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			svc := &stubKBOnlyService{getByID: func(context.Context, string) (*types.KnowledgeBase, error) {
				return tt.kb, nil
			}}
			kg := &handlerKnowledgeAccessStub{
				knowledge: &types.Knowledge{ID: "doc", KnowledgeBaseID: "kb", TenantID: tt.kb.TenantID},
			}
			h := &KnowledgeHandler{kbService: svc, kgService: kg, kbAccessGrantService: &handlerKBGrantStub{}}
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodGet, "/", nil).WithContext(
				types.WithCaller(context.Background(), types.Caller{UserID: "guest-1"}))

			_, _, err := h.resolveKnowledgeAndValidateKBAccess(c, "doc", types.KBPermissionViewer)
			require.Equal(t, tt.readOK, err == nil, "viewer err=%v", err)
			_, _, err = h.resolveKnowledgeAndValidateKBDownload(c, "doc")
			require.Equal(t, tt.downOK, err == nil, "download err=%v", err)
			_, _, err = h.resolveKnowledgeAndValidateKBAccess(c, "doc", types.KBPermissionEditor)
			require.Equal(t, tt.editorOK, err == nil, "editor err=%v", err)
		})
	}
}
