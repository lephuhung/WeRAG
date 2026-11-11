package handler

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
)

func TestStripReservedUploadMetadata(t *testing.T) {
	meta := func() map[string]string {
		return map[string]string{"external_id": "law", "source": "vbpl", "doc_number": "31/2024/QH15"}
	}
	human := func(tenant uint64, role types.TenantRole) context.Context {
		ctx := types.WithPrincipal(context.Background(), types.Principal{Type: types.PrincipalWebUser, ID: "u"})
		return types.WithCaller(ctx, types.Caller{TenantID: tenant, UserID: "u", Role: role})
	}
	key := func(tenant uint64) context.Context {
		ctx := types.WithCaller(context.Background(), types.Caller{TenantID: tenant, UserID: "system-1", Role: types.TenantRoleAdmin})
		return types.WithTenantAPIKeyScope(ctx, types.TenantAPIKeyScope{KeyID: 1, FullAccess: true})
	}
	superAdmin := context.WithValue(human(7, ""), types.SystemAdminContextKey, true)

	for name, ctx := range map[string]context.Context{
		"member":            human(7, types.TenantRoleMember),
		"tenant admin":      human(7, types.TenantRoleAdmin),
		"tenant API key":    key(7),
		"guest (no tenant)": human(0, ""),
	} {
		got := stripReservedUploadMetadata(ctx, meta())
		if _, ok := got["external_id"]; ok {
			t.Errorf("%s kept external_id", name)
		}
		if _, ok := got["source"]; ok {
			t.Errorf("%s kept source", name)
		}
		if got["doc_number"] != "31/2024/QH15" {
			t.Errorf("%s lost an ordinary key", name)
		}
	}
	for name, ctx := range map[string]context.Context{
		"SuperAdmin":              superAdmin,
		"platform-tenant API key": key(types.PlatformTenantID),
	} {
		got := stripReservedUploadMetadata(ctx, meta())
		if got["external_id"] != "law" || got["source"] != "vbpl" {
			t.Errorf("%s (the vbpl importer) lost the reserved keys: %v", name, got)
		}
	}
	if stripReservedUploadMetadata(human(7, types.TenantRoleMember), nil) != nil {
		t.Error("nil metadata stays nil")
	}
}
