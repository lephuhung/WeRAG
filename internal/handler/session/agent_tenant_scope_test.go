package session

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/application/service"
	"github.com/Tencent/WeKnora/internal/event"
	"github.com/Tencent/WeKnora/internal/middleware"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

// scopeSessions owns one session with earlier turns; reaching AgentQA or
// KnowledgeQA means the turn ran.
type scopeSessions struct {
	interfaces.SessionService
	ran bool
}

func (s *scopeSessions) GetOwnedSession(_ context.Context, id string) (*types.Session, error) {
	return &types.Session{ID: id, TenantID: 2, Title: "cuộc trò chuyện cũ"}, nil
}

func (s *scopeSessions) AgentQA(context.Context, *types.QARequest, *event.EventBus) error {
	s.ran = true
	return nil
}

func (s *scopeSessions) KnowledgeQA(context.Context, *types.QARequest, *event.EventBus) error {
	s.ran = true
	return nil
}

// The real agent service and repository: profile 9 holds "group", whose
// "Tổ chức áp dụng" list was just narrowed to workspace 1, so workspace 2's
// existing conversation with it may not take another turn.
func scopeAgentService(t *testing.T) interfaces.CustomAgentService {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&types.CustomAgent{}))
	require.NoError(t, db.Exec(`CREATE TABLE tenants (id INTEGER PRIMARY KEY, kind TEXT NOT NULL DEFAULT 'workspace',
		config_profile_id INTEGER, deleted_at DATETIME)`).Error)
	require.NoError(t, db.Exec(`INSERT INTO tenants (id, kind, config_profile_id) VALUES
		(9, 'config_profile', NULL), (1, 'workspace', 9), (2, 'workspace', 9)`).Error)
	repo := repository.NewCustomAgentRepository(db)
	require.NoError(t, repo.CreateAgent(context.Background(), &types.CustomAgent{
		ID: "group", Name: "Thuế", TenantID: 9, AppliesToTenantIDs: types.AgentTenantScope{1},
		Config: types.CustomAgentConfig{AgentMode: types.AgentModeSmartReasoning},
	}))
	return service.NewCustomAgentService(repo, nil, nil, nil, nil, nil, nil)
}

func postAgentTurn(t *testing.T, h *Handler, body string) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(middleware.ErrorHandler())
	router.Use(func(c *gin.Context) {
		ctx := context.WithValue(c.Request.Context(), types.TenantIDContextKey, uint64(2))
		ctx = types.WithCaller(ctx, types.Caller{TenantID: 2, UserID: "user-2", Role: types.TenantRoleMember})
		c.Set(types.TenantIDContextKey.String(), uint64(2))
		c.Request = c.Request.WithContext(ctx)
	})
	router.POST("/agent-chat/:session_id", h.AgentQA)
	router.POST("/knowledge-chat/:session_id", h.KnowledgeQA)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/agent-chat/s-old", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)
	return rec
}

func TestAgentTurnRefusedWhenTenantLostAgent(t *testing.T) {
	sessions := &scopeSessions{}
	h := &Handler{sessionService: sessions, customAgentService: scopeAgentService(t)}

	for _, body := range []string{
		// agent mode, and the same id sent with agent mode off: neither may
		// fall back to the default configuration
		`{"query":"tiếp tục","agent_id":"group","agent_enabled":true}`,
		`{"query":"tiếp tục","agent_id":"group","agent_enabled":false}`,
		// an id that never existed answers the same
		`{"query":"tiếp tục","agent_id":"no-such-agent","agent_enabled":false}`,
	} {
		rec := postAgentTurn(t, h, body)
		require.Equal(t, http.StatusNotFound, rec.Code, rec.Body.String())
		require.Contains(t, rec.Body.String(), "không áp dụng cho Tổ chức của bạn")
		require.False(t, sessions.ran, "the turn must not run")
	}
}

func TestUnavailableAgentErrorKeepsBuiltinFallback(t *testing.T) {
	require.NoError(t, unavailableAgentError("", nil))
	require.NoError(t, unavailableAgentError(types.BuiltinQuickAnswerID, nil),
		"a switched-off built-in still answers as plain chat")
	require.NoError(t, unavailableAgentError("group", &types.CustomAgent{ID: "group"}))
	require.Error(t, unavailableAgentError("group", nil))
}
