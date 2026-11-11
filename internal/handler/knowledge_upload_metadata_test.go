package handler

import (
	"context"
	"net/http"
	"testing"

	apperrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/types"
)

func TestCheckReservedUploadMetadata(t *testing.T) {
	reserved := map[string]string{"external_id": "law", "source": "vbpl", "doc_number": "31/2024/QH15"}
	plain := map[string]string{"doc_number": "31/2024/QH15"}
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
		err := checkReservedUploadMetadata(ctx, reserved)
		appErr, ok := err.(*apperrors.AppError)
		if !ok || appErr.HTTPCode != http.StatusForbidden {
			t.Errorf("%s: err = %v, want 403", name, err)
		}
		if err := checkReservedUploadMetadata(ctx, map[string]string{"source": "x"}); err == nil {
			t.Errorf("%s: source alone must be refused too", name)
		}
		if err := checkReservedUploadMetadata(ctx, plain); err != nil {
			t.Errorf("%s: ordinary metadata refused: %v", name, err)
		}
		if err := checkReservedUploadMetadata(ctx, nil); err != nil {
			t.Errorf("%s: no metadata refused: %v", name, err)
		}
	}
	for name, ctx := range map[string]context.Context{
		"SuperAdmin":              superAdmin,
		"platform-tenant API key": key(types.PlatformTenantID),
	} {
		if err := checkReservedUploadMetadata(ctx, reserved); err != nil {
			t.Errorf("%s (the vbpl importer) refused: %v", name, err)
		}
	}
}
