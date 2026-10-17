package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	apperrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/middleware"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

type stubKBTransferService struct {
	interfaces.KnowledgeBaseService
	err       error
	gotID     string
	gotTarget uint64
}

func (s *stubKBTransferService) TransferKnowledgeBaseOwner(
	_ context.Context, id string, target uint64,
) (*types.KBOwnerTransferResult, error) {
	s.gotID, s.gotTarget = id, target
	if s.err != nil {
		return nil, s.err
	}
	return &types.KBOwnerTransferResult{
		KnowledgeBase: &types.KnowledgeBase{
			ID: id, TenantID: 1, OwnerTenantID: target, Visibility: types.KBVisibilityTenant,
		},
		PreviousOwnerTenantID: 1, PreviousVisibility: types.KBVisibilityPublished,
		RevokedInvitations: 4, RevokedGrants: 1, WithdrawnFromPublished: true,
	}, nil
}

func serveTransfer(t *testing.T, svc interfaces.KnowledgeBaseService, body string) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(middleware.ErrorHandler())
	h := &KnowledgeBaseHandler{service: svc}
	r.POST("/knowledge-bases/:id/transfer-owner", h.TransferKnowledgeBaseOwner)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/knowledge-bases/kb-1/transfer-owner", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	return w
}

func TestTransferKnowledgeBaseOwnerHandler(t *testing.T) {
	svc := &stubKBTransferService{}
	w := serveTransfer(t, svc, `{"target_tenant_id": 7}`)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Equal(t, "kb-1", svc.gotID)
	assert.Equal(t, uint64(7), svc.gotTarget)
	var resp struct {
		Success bool `json:"success"`
		Data    struct {
			KnowledgeBase struct {
				ID            string `json:"id"`
				OwnerTenantID uint64 `json:"owner_tenant_id"`
				TenantID      uint64 `json:"tenant_id"`
				Visibility    string `json:"visibility"`
			} `json:"knowledge_base"`
			PreviousOwnerTenantID  uint64 `json:"previous_owner_tenant_id"`
			PreviousVisibility     string `json:"previous_visibility"`
			RevokedInvitations     int64  `json:"revoked_invitations"`
			RevokedGrants          int64  `json:"revoked_grants"`
			WithdrawnFromPublished bool   `json:"withdrawn_from_published"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.True(t, resp.Success)
	assert.Equal(t, "kb-1", resp.Data.KnowledgeBase.ID)
	assert.Equal(t, uint64(7), resp.Data.KnowledgeBase.OwnerTenantID)
	assert.Equal(t, uint64(1), resp.Data.KnowledgeBase.TenantID)
	assert.Equal(t, "tenant", resp.Data.KnowledgeBase.Visibility)
	assert.Equal(t, uint64(1), resp.Data.PreviousOwnerTenantID)
	assert.Equal(t, "published", resp.Data.PreviousVisibility)
	assert.Equal(t, int64(4), resp.Data.RevokedInvitations)
	assert.Equal(t, int64(1), resp.Data.RevokedGrants)
	assert.True(t, resp.Data.WithdrawnFromPublished)
}

func TestTransferKnowledgeBaseOwnerHandlerErrors(t *testing.T) {
	w := serveTransfer(t, &stubKBTransferService{}, `{}`)
	assert.Equal(t, http.StatusBadRequest, w.Code, "target_tenant_id is required")

	for code, err := range map[int]error{
		http.StatusForbidden:  apperrors.NewForbiddenError("no"),
		http.StatusNotFound:   apperrors.NewNotFoundError("target tenant not found"),
		http.StatusBadRequest: apperrors.NewBadRequestError("same owner"),
	} {
		w := serveTransfer(t, &stubKBTransferService{err: err}, `{"target_tenant_id": 2}`)
		assert.Equal(t, code, w.Code, w.Body.String())
	}
}
