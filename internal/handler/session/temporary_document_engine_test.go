package session

import (
	"bytes"
	"context"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type engineTestSessions struct{ interfaces.SessionService }

func (engineTestSessions) GetOwnedSession(context.Context, string) (*types.Session, error) {
	return &types.Session{ID: "session-1", TenantID: 7}, nil
}

type engineTestAgents struct {
	interfaces.CustomAgentService
	agent *types.CustomAgent
	err   error
}

func (s *engineTestAgents) GetAgentByID(context.Context, string) (*types.CustomAgent, error) {
	return s.agent, s.err
}

type engineTestDocuments struct {
	interfaces.TemporaryDocumentService
	options types.TemporaryDocumentCreateOptions
}

func (d *engineTestDocuments) Create(
	_ context.Context, _ uint64, _, _, _ string, _ int64, _ io.Reader, options types.TemporaryDocumentCreateOptions,
) (*types.TemporaryDocument, error) {
	d.options = options
	return &types.TemporaryDocument{ID: "doc-1"}, nil
}

func uploadWithEngine(t *testing.T, h *Handler, agentID string) {
	t.Helper()
	body := &bytes.Buffer{}
	form := multipart.NewWriter(body)
	part, err := form.CreateFormFile("file", "report.pdf")
	require.NoError(t, err)
	_, _ = part.Write([]byte("%PDF-1.4"))
	require.NoError(t, form.WriteField("parser_engine", "mineru_cloud"))
	require.NoError(t, form.WriteField("agent_id", agentID))
	require.NoError(t, form.Close())

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Set(types.TenantIDContextKey.String(), uint64(7))
	c.Params = gin.Params{{Key: "session_id", Value: "session-1"}}
	req := httptest.NewRequest(http.MethodPost, "/", body).WithContext(
		context.WithValue(context.Background(), types.TenantIDContextKey, uint64(7)))
	req.Header.Set("Content-Type", form.FormDataContentType())
	c.Request = req
	h.UploadTemporaryDocument(c)
	require.Empty(t, c.Errors)
}

// Cross-tenant agents no longer resolve: the caller's engine choice applies to
// its own agents, and an agent ID that does not resolve in the caller's tenant
// is ignored rather than silently switching execution scope.
func TestUploadTemporaryDocumentEngineSelection(t *testing.T) {
	own := &engineTestDocuments{}
	h := &Handler{
		sessionService:     engineTestSessions{},
		temporaryDocuments: own,
		customAgentService: &engineTestAgents{agent: &types.CustomAgent{ID: "own-agent", TenantID: 7}},
	}
	uploadWithEngine(t, h, "own-agent")
	require.Equal(t, "mineru_cloud", own.options.ParserEngine)
	require.Zero(t, own.options.ResourceTenantID)

	unknown := &engineTestDocuments{}
	h.temporaryDocuments = unknown
	h.customAgentService = &engineTestAgents{err: context.Canceled}
	uploadWithEngine(t, h, "foreign-agent")
	require.Equal(t, "mineru_cloud", unknown.options.ParserEngine,
		"an unresolvable agent ID must not strip the caller's engine choice")
	require.Zero(t, unknown.options.ResourceTenantID)
}
