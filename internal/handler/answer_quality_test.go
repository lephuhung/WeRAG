package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	apperrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/middleware"
	"github.com/Tencent/WeKnora/internal/types"
)

type capturingAnswerQuality struct {
	platform bool
	filter   types.AnswerQualityFilter
	err      error
}

func (s *capturingAnswerQuality) ListDisliked(_ context.Context, p bool, f types.AnswerQualityFilter) (*types.AnswerQualityPage, error) {
	s.platform, s.filter = p, f
	return &types.AnswerQualityPage{}, s.err
}
func (s *capturingAnswerQuality) ListUngrounded(_ context.Context, p bool, f types.AnswerQualityFilter) (*types.AnswerQualityPage, error) {
	s.platform, s.filter = p, f
	return &types.AnswerQualityPage{}, s.err
}
func (s *capturingAnswerQuality) GroupUngrounded(_ context.Context, p bool, f types.AnswerQualityFilter) (*types.UngroundedGroupPage, error) {
	s.platform, s.filter = p, f
	return &types.UngroundedGroupPage{}, s.err
}
func (s *capturingAnswerQuality) Review(context.Context, bool, string, types.AnswerQualityReviewInput) (*types.AnswerQualityReview, error) {
	return &types.AnswerQualityReview{}, s.err
}

func answerQualityRouter(svc *capturingAnswerQuality) *gin.Engine {
	gin.SetMode(gin.TestMode)
	h := NewAnswerQualityHandler(svc)
	r := gin.New()
	r.Use(middleware.ErrorHandler())
	r.GET("/quality/disliked", h.ListDisliked)
	r.GET("/quality/ungrounded", h.ListUngrounded)
	r.GET("/system/quality/disliked", h.PlatformListDisliked)
	return r
}

func getStatus(r *gin.Engine, path string) int {
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
	return w.Code
}

func TestAnswerQualityHandlerFilters(t *testing.T) {
	svc := &capturingAnswerQuality{}
	r := answerQualityRouter(svc)

	require.Equal(t, http.StatusOK, getStatus(r,
		"/quality/disliked?from=2026-11-01&to=2026-11-10&agent_id=a1&has_comment=true&reviewed=false&page=3&page_size=10&tenant_id=7"))
	require.False(t, svc.platform)
	f := svc.filter
	require.Equal(t, time.Date(2026, 11, 1, 0, 0, 0, 0, time.Local), *f.From)
	require.Equal(t, time.Date(2026, 11, 11, 0, 0, 0, 0, time.Local), *f.To, "the to day is included")
	require.Equal(t, "a1", f.AgentID)
	require.True(t, f.HasComment)
	require.NotNil(t, f.Reviewed)
	require.False(t, *f.Reviewed)
	require.Equal(t, 20, f.Offset)
	require.Equal(t, 10, f.Limit)
	require.Zero(t, f.TenantID, "tenant_id is read on the console only")

	require.Equal(t, http.StatusOK, getStatus(r, "/system/quality/disliked?tenant_id=7"))
	require.True(t, svc.platform)
	require.EqualValues(t, 7, svc.filter.TenantID)

	for _, bad := range []string{
		"/quality/disliked?from=yesterday",
		"/quality/disliked?page_size=500",
		"/quality/disliked?page=0",
		"/quality/ungrounded?reason=whatever",
		"/system/quality/disliked?tenant_id=x",
	} {
		require.Equal(t, http.StatusBadRequest, getStatus(r, bad), bad)
	}

	svc.err = apperrors.NewForbiddenError("organization admin required")
	require.Equal(t, http.StatusForbidden, getStatus(r, "/quality/disliked"))
}
