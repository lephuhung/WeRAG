package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	apperrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/ratelimit"
	"github.com/Tencent/WeKnora/internal/types"
)

// legalLookupCaller is the identity the test's fake auth attaches.
type legalLookupCaller struct {
	userID string
	role   types.TenantRole
	apiKey bool
}

// legalLookupTestRouter mounts the lookup guard (in-memory limiter, guest
// 3/IP, member 5/account) behind a fake auth step that reads the caller
// from the test, and a minimal error renderer.
func legalLookupTestRouter(who func() legalLookupCaller) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Next()
		if len(c.Errors) > 0 {
			if e, ok := c.Errors.Last().Err.(*apperrors.AppError); ok {
				c.AbortWithStatus(e.HTTPCode)
			}
		}
	})
	r.Use(func(c *gin.Context) {
		w := who()
		ctx := c.Request.Context()
		if w.userID != "" {
			principal := types.Principal{Type: types.PrincipalWebUser, ID: w.userID}
			if w.apiKey {
				principal = types.Principal{Type: types.PrincipalAPITenant, ID: "7"}
				ctx = types.WithTenantAPIKeyScope(ctx, types.TenantAPIKeyScope{KeyID: 1, FullAccess: true})
			}
			ctx = types.WithPrincipal(ctx, principal)
			ctx = types.WithCaller(ctx, types.Caller{TenantID: 7, UserID: w.userID, Role: w.role})
		}
		c.Request = c.Request.WithContext(ctx)
		c.Next()
	})
	limiter := ratelimit.New(nil, "test:", legalLookupRateWindow, "test")
	r.GET("/legal-lookup", legalLookupAccess(limiter, 3, 5), func(c *gin.Context) { c.Status(http.StatusOK) })
	return r
}

func getLookup(r *gin.Engine, ip string) int {
	req := httptest.NewRequest(http.MethodGet, "/legal-lookup?q=53", nil)
	req.RemoteAddr = ip + ":4000"
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w.Code
}

func TestLegalLookupGuestLimitedPerIP(t *testing.T) {
	user := "guest-1"
	r := legalLookupTestRouter(func() legalLookupCaller { return legalLookupCaller{userID: user} })
	for i := 0; i < 3; i++ {
		if code := getLookup(r, "203.0.113.7"); code != http.StatusOK {
			t.Fatalf("request %d = %d, want 200", i, code)
		}
	}
	if code := getLookup(r, "203.0.113.7"); code != http.StatusTooManyRequests {
		t.Fatalf("4th request = %d, want 429", code)
	}
	// another account behind the same IP shares the budget
	user = "guest-2"
	if code := getLookup(r, "203.0.113.7"); code != http.StatusTooManyRequests {
		t.Fatalf("new guest on the same IP = %d, want 429", code)
	}
	// another IP has its own
	if code := getLookup(r, "198.51.100.4"); code != http.StatusOK {
		t.Fatalf("other IP = %d, want 200", code)
	}
}

func TestLegalLookupMemberLimitedPerAccount(t *testing.T) {
	user := "member-1"
	r := legalLookupTestRouter(func() legalLookupCaller {
		return legalLookupCaller{userID: user, role: types.TenantRoleMember}
	})
	// a member is not held to the guest budget, and an IP change does not help
	for i := 0; i < 5; i++ {
		if code := getLookup(r, "203.0.113."+string(rune('1'+i))); code != http.StatusOK {
			t.Fatalf("request %d = %d, want 200", i, code)
		}
	}
	if code := getLookup(r, "198.51.100.9"); code != http.StatusTooManyRequests {
		t.Fatalf("6th request = %d, want 429", code)
	}
	user = "member-2"
	if code := getLookup(r, "203.0.113.1"); code != http.StatusOK {
		t.Fatalf("other member = %d, want 200", code)
	}
}

func TestLegalLookupRefusesMachinesAndAnonymous(t *testing.T) {
	anon := legalLookupTestRouter(func() legalLookupCaller { return legalLookupCaller{} })
	if code := getLookup(anon, "203.0.113.7"); code != http.StatusForbidden {
		t.Fatalf("anonymous = %d, want 403", code)
	}
	key := legalLookupTestRouter(func() legalLookupCaller {
		return legalLookupCaller{userID: "system-7", role: types.TenantRoleAdmin, apiKey: true}
	})
	if code := getLookup(key, "203.0.113.7"); code != http.StatusForbidden {
		t.Fatalf("API key = %d, want 403", code)
	}
}

func TestLegalLookupIsTenantOptional(t *testing.T) {
	if !isTenantOptionalAPI("/api/v1/legal-lookup", http.MethodGet) {
		t.Fatal("GET /legal-lookup must be reachable without an organization (Guests)")
	}
	if isTenantOptionalAPI("/api/v1/legal-lookup", http.MethodPost) || isTenantOptionalAPI("/api/v1/legal-lookup/x", http.MethodGet) {
		t.Fatal("only GET /legal-lookup is tenant-optional")
	}
}

// Through the real auth step: a Guest (no organization) reaches the lookup
// tenantless and is held to the per-IP budget; a member of tenant 7 reaches
// it in the organization with the account budget.
func TestLegalLookupThroughAuth(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tenants := &portalTenantService{tenants: map[uint64]*types.Tenant{
		7:            {ID: 7, Name: "Sở Tư pháp", Kind: types.TenantKindWorkspace},
		testPortalID: {ID: testPortalID, Name: "Tra cứu công khai", Kind: types.TenantKindPublicPortal},
	}}
	members := newFakeMemberService()
	members.seedActive("member", 7, types.TenantRoleMember)
	engine := func(user *types.User) *gin.Engine {
		r := gin.New()
		r.Use(func(c *gin.Context) {
			c.Next()
			if len(c.Errors) > 0 {
				if e, ok := c.Errors.Last().Err.(*apperrors.AppError); ok {
					c.AbortWithStatus(e.HTTPCode)
				}
			}
		})
		r.Use(func(c *gin.Context) {
			if authenticateJWTUser(c, tenants, members, cfgWithRBAC(true), user, user.TenantID) {
				c.Next()
			}
		})
		limiter := ratelimit.New(nil, "test:", legalLookupRateWindow, "test")
		r.GET("/api/v1/legal-lookup", legalLookupAccess(limiter, 2, 4), func(c *gin.Context) {
			tid, _ := types.TenantIDFromContext(c.Request.Context())
			if tid != 0 && tid != 7 {
				t.Errorf("served in tenant %d", tid)
			}
			c.Status(http.StatusOK)
		})
		return r
	}
	get := func(r *gin.Engine) int {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/legal-lookup?q=53", nil)
		req.RemoteAddr = "203.0.113.7:4000"
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w.Code
	}

	guest := engine(&types.User{ID: "guest", IsActive: true})
	for i, want := range []int{200, 200, 429} {
		if code := get(guest); code != want {
			t.Fatalf("guest request %d = %d, want %d", i, code, want)
		}
	}
	member := engine(&types.User{ID: "member", TenantID: 7, IsActive: true})
	for i, want := range []int{200, 200, 200, 200, 429} {
		if code := get(member); code != want {
			t.Fatalf("member request %d = %d, want %d", i, code, want)
		}
	}
}
