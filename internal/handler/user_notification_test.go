package handler

import (
	"context"
	"encoding/json"
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
	"github.com/Tencent/WeKnora/internal/middleware"
	"github.com/Tencent/WeKnora/internal/types"
)

type notificationCaller struct {
	userID string
	apiKey bool
	portal bool
}

func newNotificationRouter(t *testing.T) (*gin.Engine, *gorm.DB, *notificationCaller) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&types.UserNotification{}))
	h := NewUserNotificationHandler(repository.NewUserNotificationRepository(db))
	who := &notificationCaller{}
	r := gin.New()
	r.Use(middleware.ErrorHandler(), func(c *gin.Context) {
		ctx := c.Request.Context()
		if who.userID != "" {
			ctx = context.WithValue(ctx, types.UserIDContextKey, who.userID)
		}
		if who.apiKey {
			ctx = types.WithTenantAPIKeyScope(ctx, types.TenantAPIKeyScope{KeyID: 1, FullAccess: true})
		}
		if who.portal {
			ctx = context.WithValue(ctx, types.TenantInfoContextKey, &types.Tenant{ID: 10012, Kind: types.TenantKindPublicPortal})
		}
		c.Request = c.Request.WithContext(ctx)
		c.Next()
	})
	g := r.Group("/api/v1/notifications")
	g.GET("", h.List)
	g.GET("/unread-count", h.UnreadCount)
	g.POST("/seen", h.Seen)
	g.POST("/read-all", h.ReadAll)
	g.POST("/:id/read", h.Read)
	return r, db, who
}

func notificationCall(t *testing.T, r *gin.Engine, method, path, body string) (int, map[string]any) {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	var out map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	return w.Code, out
}

func TestUserNotificationAPIOwnRowsOnly(t *testing.T) {
	r, db, who := newNotificationRouter(t)
	for _, n := range []types.UserNotification{
		{ID: "a1", UserID: "an", Kind: "k", Title: "Văn bản A đã được bổ sung", Link: "/platform/chat/s1", DedupeKey: "a1"},
		{ID: "b1", UserID: "binh", Kind: "k", Title: "B", DedupeKey: "b1"},
	} {
		require.NoError(t, db.Create(&n).Error)
	}

	who.userID = "an"
	code, out := notificationCall(t, r, http.MethodGet, "/api/v1/notifications?unread=true&limit=10", "")
	require.Equal(t, http.StatusOK, code)
	items := out["data"].(map[string]any)["items"].([]any)
	require.Len(t, items, 1)
	first := items[0].(map[string]any)
	require.Equal(t, "a1", first["id"])
	require.Equal(t, "/platform/chat/s1", first["link"])
	require.NotContains(t, first, "user_id")
	require.NotContains(t, first, "dedupe_key")

	code, out = notificationCall(t, r, http.MethodGet, "/api/v1/notifications/unread-count", "")
	require.Equal(t, http.StatusOK, code)
	require.EqualValues(t, map[string]any{"unread": 1.0, "unseen": 1.0}, out["data"])

	// someone else's id: ignored / not found
	code, out = notificationCall(t, r, http.MethodPost, "/api/v1/notifications/seen", `{"ids":["a1","b1"]}`)
	require.Equal(t, http.StatusOK, code)
	require.EqualValues(t, 1, out["data"].(map[string]any)["updated"])
	code, _ = notificationCall(t, r, http.MethodPost, "/api/v1/notifications/b1/read", "")
	require.Equal(t, http.StatusNotFound, code)
	code, _ = notificationCall(t, r, http.MethodPost, "/api/v1/notifications/a1/read", "")
	require.Equal(t, http.StatusOK, code)
	code, _ = notificationCall(t, r, http.MethodPost, "/api/v1/notifications/read-all", "")
	require.Equal(t, http.StatusOK, code)

	var b types.UserNotification
	require.NoError(t, db.Where("id = ?", "b1").Take(&b).Error)
	require.Nil(t, b.SeenAt)
	require.Nil(t, b.ReadAt)

	code, _ = notificationCall(t, r, http.MethodGet, "/api/v1/notifications?limit=500", "")
	require.Equal(t, http.StatusBadRequest, code)
	code, _ = notificationCall(t, r, http.MethodGet, "/api/v1/notifications?before=yesterday", "")
	require.Equal(t, http.StatusBadRequest, code)
}

// A Guest in the public portal reads their own notifications; an API key
// and an anonymous caller are refused.
func TestUserNotificationAPIGuestAndAPIKey(t *testing.T) {
	r, db, who := newNotificationRouter(t)
	require.NoError(t, db.Create(&types.UserNotification{ID: "g1", UserID: "guest", Kind: "k", DedupeKey: "g1"}).Error)

	who.userID, who.portal = "guest", true
	code, out := notificationCall(t, r, http.MethodGet, "/api/v1/notifications", "")
	require.Equal(t, http.StatusOK, code)
	require.Len(t, out["data"].(map[string]any)["items"], 1)

	who.portal, who.apiKey = false, true
	for _, c := range []struct{ method, path string }{
		{http.MethodGet, "/api/v1/notifications"}, {http.MethodGet, "/api/v1/notifications/unread-count"},
		{http.MethodPost, "/api/v1/notifications/read-all"},
	} {
		code, _ = notificationCall(t, r, c.method, c.path, "")
		require.Equal(t, http.StatusForbidden, code, c.path)
	}

	who.apiKey, who.userID = false, ""
	code, _ = notificationCall(t, r, http.MethodGet, "/api/v1/notifications/unread-count", "")
	require.Equal(t, http.StatusUnauthorized, code)
}
