package middleware

import (
	"errors"
	"net/http"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"

	"github.com/Tencent/WeKnora/internal/types"
)

func uploaderLookup(uploader string, err error) (UploaderLookup, *int) {
	calls := 0
	return func(*gin.Context) (string, error) {
		calls++
		return uploader, err
	}, &calls
}

func TestRequireTenantAdminOrUploader(t *testing.T) {
	cases := []struct {
		name     string
		role     types.TenantRole
		userID   string
		uploader string
		err      error
		want     int
	}{
		{"admin edits any document", types.TenantRoleAdmin, "u-admin", "u-other", nil, http.StatusOK},
		{"member edits own upload", types.TenantRoleMember, "u1", "u1", nil, http.StatusOK},
		{"member cannot edit another's upload", types.TenantRoleMember, "u1", "u2", nil, http.StatusForbidden},
		{"legacy document without uploader is admin-only", types.TenantRoleMember, "u1", "", nil, http.StatusForbidden},
		{"unresolved document reaches the 404 path", types.TenantRoleMember, "u1", "", ErrResourceNotFound, http.StatusOK},
		{"lookup failure is 503", types.TenantRoleMember, "u1", "", errors.New("db down"), http.StatusServiceUnavailable},
		{"synthetic API user never matches", types.TenantRoleMember, "system-7", "system-7", nil, http.StatusForbidden},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			lookup, _ := uploaderLookup(tc.uploader, tc.err)
			w := rbacTestHarness(tc.role, tc.userID, RequireTenantAdminOrUploader(lookup, cfgRBAC(true)))
			assert.Equal(t, tc.want, w.Code)
		})
	}
}

func TestRequireTenantAdminOrUploader_EnforcedWhenRBACDisabled(t *testing.T) {
	lookup, _ := uploaderLookup("u2", nil)
	w := rbacTestHarness(types.TenantRoleMember, "u1", RequireTenantAdminOrUploader(lookup, cfgRBAC(false)))
	assert.Equal(t, http.StatusForbidden, w.Code)
}

func TestRequireTenantAdminOrUploader_AdminSkipsLookup(t *testing.T) {
	lookup, calls := uploaderLookup("", errors.New("must not be called"))
	w := rbacTestHarness(types.TenantRoleAdmin, "u-admin", RequireTenantAdminOrUploader(lookup, cfgRBAC(true)))
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, 0, *calls)
}
