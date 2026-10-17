package session

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/application/service"
	"github.com/Tencent/WeKnora/internal/middleware"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// contextUsageMessages serves one completed turn of every session.
type contextUsageMessages struct {
	interfaces.MessageRepository
	listed []string
}

func (r *contextUsageMessages) ListMessagesBySessionBeforeCursor(
	_ context.Context, sessionID string, before time.Time, beforeID string, _ int,
) ([]*types.Message, error) {
	r.listed = append(r.listed, sessionID)
	if !before.IsZero() || beforeID != "" {
		return nil, nil
	}
	at := time.Date(2026, 10, 15, 9, 0, 0, 0, time.UTC)
	return []*types.Message{
		{ID: "a1", RequestID: "r1", Role: "assistant", Content: "answer", IsCompleted: true, CreatedAt: at.Add(time.Second)},
		{ID: "u1", RequestID: "r1", Role: "user", Content: "question", CreatedAt: at},
	}, nil
}

func (r *contextUsageMessages) GetLatestContextCheckpoint(context.Context, string) (*types.Message, error) {
	return nil, nil
}

func newContextUsageRoute(messages *contextUsageMessages) *gin.Engine {
	gin.SetMode(gin.TestMode)
	h := &Handler{
		sessionService: &ownerOnlySessions{owned: map[string]bool{"mine": true, "theirs": false}},
		contextUsage:   service.NewSessionContextUsageService(nil, messages, nil, nil, nil, nil, nil),
	}
	r := gin.New()
	r.Use(middleware.ErrorHandler())
	r.GET("/sessions/:id/context-usage", h.GetContextUsage)
	return r
}

// Another user's session, another workspace's or a missing one is 404, and
// nothing of it is read.
func TestGetContextUsageOnlyForTheOwner(t *testing.T) {
	messages := &contextUsageMessages{}
	r := newContextUsageRoute(messages)
	for _, id := range []string{"theirs", "missing"} {
		w := doJSON(r, http.MethodGet, "/sessions/"+id+"/context-usage", "", nil)
		require.Equal(t, http.StatusNotFound, w.Code, id)
	}
	require.Empty(t, messages.listed)

	w := doJSON(r, http.MethodGet, "/sessions/mine/context-usage", "", nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var body struct {
		Data types.SessionContextUsage `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	require.Positive(t, body.Data.HistoryTokens)
	require.Equal(t, types.DefaultHistoryTokens, body.Data.HistoryBudget, "no plan: the default history budget")
	require.Equal(t, []string{"mine", "mine"}, messages.listed)
}
