package session

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	apperrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/gin-gonic/gin"
)

type continueSessions struct {
	interfaces.SessionService
	owned     map[string]*types.Session
	continued []string
}

func (s *continueSessions) GetOwnedSession(_ context.Context, id string) (*types.Session, error) {
	if sess, ok := s.owned[id]; ok {
		return sess, nil
	}
	return nil, apperrors.ErrSessionNotFound
}

func (s *continueSessions) ContinueSession(_ context.Context, old *types.Session) (*types.Session, error) {
	s.continued = append(s.continued, old.ID)
	return &types.Session{ID: old.ID + "-next", TenantID: old.TenantID}, nil
}

func runContinue(h *Handler, sessionID string) (*httptest.ResponseRecorder, *gin.Context) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/sessions/"+sessionID+"/continue", nil)
	c.Params = gin.Params{{Key: "session_id", Value: sessionID}}
	h.ContinueSession(c)
	return w, c
}

// Only the owner can carry a session on; the new session comes back.
func TestContinueSessionHandlerChecksOwnership(t *testing.T) {
	sessions := &continueSessions{owned: map[string]*types.Session{"mine": {ID: "mine", TenantID: 1}}}
	h := &Handler{sessionService: sessions}

	w, _ := runContinue(h, "mine")
	if w.Code != http.StatusOK {
		t.Fatalf("owner: want 200, got %d %s", w.Code, w.Body.String())
	}
	var body struct {
		Data types.Session `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil || body.Data.ID != "mine-next" {
		t.Fatalf("want the new session, got %s (%v)", w.Body.String(), err)
	}

	// errors are rendered by the error middleware; the handler records them
	_, c := runContinue(h, "theirs")
	if len(sessions.continued) != 1 {
		t.Fatalf("a session the caller does not own must not be continued: %v", sessions.continued)
	}
	if len(c.Errors) != 1 {
		t.Fatalf("not owner: want a not-found error, got %v", c.Errors)
	}
	var appErr *apperrors.AppError
	if !errors.As(c.Errors[0].Err, &appErr) || appErr.HTTPCode != http.StatusNotFound {
		t.Fatalf("not owner: want 404, got %v", c.Errors[0].Err)
	}
}
