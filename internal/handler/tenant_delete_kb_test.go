package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/middleware"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// Task 3: deleting a tenant blocked by an active KB data-scope dependency
// surfaces HTTP 409 with the blocking KB ID; unrelated failures keep their
// existing mapping.
type blockingTenantService struct {
	stubTenantService
	deleteErr error
}

func (s *blockingTenantService) DeleteTenant(context.Context, uint64) error { return s.deleteErr }

func TestDeleteTenantBlockedByKBReturnsConflictWithKBID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := &TenantHandler{service: &blockingTenantService{deleteErr: &repository.TenantKBDependencyError{
		TenantID: 7, KnowledgeBaseID: "kb-blocker",
	}}}

	r := gin.New()
	r.Use(middleware.ErrorHandler())
	r.DELETE("/tenants/:id", h.DeleteTenant)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodDelete, "/tenants/7", nil)
	r.ServeHTTP(rec, req)
	require.Equal(t, http.StatusConflict, rec.Code, "body=%s", rec.Body.String())
	require.Contains(t, rec.Body.String(), "kb-blocker")
}
