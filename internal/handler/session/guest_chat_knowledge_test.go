package session

import (
	"context"
	stderrors "errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/types"
)

// /knowledge-chat never runs a Guest's turn: its quick-answer pipeline
// would not be held to the portal boundary on the agent.
func TestKnowledgeQARefusesThePublicPortal(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	ctx := context.WithValue(context.Background(), types.TenantInfoContextKey,
		&types.Tenant{ID: 10012, Kind: types.TenantKindPublicPortal})
	c.Request = httptest.NewRequest(http.MethodPost, "/knowledge-chat/s1", nil).WithContext(ctx)
	(&Handler{}).KnowledgeQA(c)
	require.Len(t, c.Errors, 1)
	var appErr *errors.AppError
	require.True(t, stderrors.As(c.Errors[0].Err, &appErr))
	require.Equal(t, http.StatusBadRequest, appErr.HTTPCode)
}
