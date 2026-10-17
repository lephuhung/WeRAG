package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Tencent/WeKnora/internal/config"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/gin-gonic/gin"
)

type membershipStateUserService struct {
	tenantPolicyUserService
	memberships []types.Membership
}

func (s *membershipStateUserService) BuildLoginMemberships(context.Context, *types.User, *types.Tenant) []types.Membership {
	return s.memberships
}

// GET /auth/me reports membership_state: SuperAdmin takes precedence, one
// active membership makes a member, none makes a guest. The legacy
// CanAccessAllTenants flag plays no part.
func TestAuthMeMembershipState(t *testing.T) {
	gin.SetMode(gin.TestMode)
	one := []types.Membership{{TenantID: 7, Role: types.TenantRoleMember}}
	cases := []struct {
		name        string
		user        *types.User
		memberships []types.Membership
		want        string
	}{
		{"superadmin without membership", &types.User{ID: "root", IsSystemAdmin: true}, nil, "superadmin"},
		{"superadmin with membership", &types.User{ID: "root", IsSystemAdmin: true}, one, "superadmin"},
		{"member", &types.User{ID: "u1"}, one, "member"},
		{"guest", &types.User{ID: "g1"}, []types.Membership{}, "guest"},
		{"legacy flag is still a guest", &types.User{ID: "g2", CanAccessAllTenants: true}, nil, "guest"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := &AuthHandler{
				userService: &membershipStateUserService{
					tenantPolicyUserService: tenantPolicyUserService{user: tc.user},
					memberships:             tc.memberships,
				},
				configInfo:       &config.Config{Tenant: &config.TenantConfig{}},
				systemSettingSvc: &tenantPolicySettingService{enabled: false},
			}
			r := gin.New()
			r.GET("/auth/me", h.GetCurrentUser)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/auth/me", nil))
			if w.Code != http.StatusOK {
				t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
			}
			var body struct {
				Data struct {
					MembershipState string `json:"membership_state"`
				} `json:"data"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
				t.Fatalf("decode: %v", err)
			}
			if body.Data.MembershipState != tc.want {
				t.Fatalf("membership_state=%q, want %q", body.Data.MembershipState, tc.want)
			}
		})
	}
}
