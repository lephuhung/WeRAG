package types

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestKnowledgeBase_OwnerTenantID_JSONName pins the API contract from the
// platform-public-KB plan: the ownership field serializes as
// `owner_tenant_id` and round-trips through the custom MarshalJSON/
// UnmarshalJSON pair on KnowledgeBase.
func TestKnowledgeBase_OwnerTenantID_JSONName(t *testing.T) {
	kb := &KnowledgeBase{ID: "kb-1", TenantID: 7, OwnerTenantID: 7}
	raw, err := json.Marshal(kb)
	require.NoError(t, err)
	assert.Contains(t, string(raw), `"owner_tenant_id":7`)

	var decoded KnowledgeBase
	require.NoError(t, json.Unmarshal([]byte(`{"id":"kb-1","tenant_id":7,"owner_tenant_id":0}`), &decoded))
	assert.Equal(t, uint64(0), decoded.OwnerTenantID)
	assert.Equal(t, uint64(7), decoded.TenantID)
}

// TestKnowledgeBase_ValidateOwnership pins the owner/visibility invariant:
// public requires platform ownership (0); tenant requires a nonzero owner.
// The data-scope TenantID is never consulted.
func TestKnowledgeBase_ValidateOwnership(t *testing.T) {
	cases := []struct {
		name      string
		owner     uint64
		tenant    uint64
		visible   KBVisibility
		wantError bool
	}{
		{"tenant owned by its data-scope tenant", 7, 7, KBVisibilityTenant, false},
		{"tenant owned while data scope differs (post-transition stability)", 9, 7, KBVisibilityTenant, false},
		{"platform public with zero data scope", 0, 0, KBVisibilityPublic, false},
		{"platform public retaining a tenant data scope", 0, 7, KBVisibilityPublic, false},
		{"public with tenant owner rejected", 7, 7, KBVisibilityPublic, true},
		{"tenant with platform owner rejected", 0, 7, KBVisibilityTenant, true},
		{"tenant with zero data scope but nonzero owner is still tenant-valid", 5, 0, KBVisibilityTenant, false},
		{"unknown visibility rejected", 7, 7, KBVisibility("org"), true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			kb := &KnowledgeBase{TenantID: tc.tenant, OwnerTenantID: tc.owner, Visibility: tc.visible}
			err := kb.ValidateOwnership()
			if tc.wantError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

// TestKnowledgeBase_EnsureDefaults_PreservesOwner guards the Task 1 / Task 3
// boundary: EnsureDefaults must not synthesize OwnerTenantID from TenantID.
// Tenant-create (Task 3) sets both explicitly; public-create sets owner 0.
// A default that folded 0 into tenant_id here would silently re-tenant
// platform rows, so the zero value must survive untouched.
func TestKnowledgeBase_EnsureDefaults_PreservesOwner(t *testing.T) {
	kb := &KnowledgeBase{TenantID: 7, Visibility: KBVisibilityTenant}
	kb.EnsureDefaults()
	assert.Equal(t, uint64(0), kb.OwnerTenantID, "EnsureDefaults must not invent an owner")
	assert.True(t, strings.Contains(kb.Type, "document"))
}
