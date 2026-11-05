package handler

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
)

// ctxWithCaller builds the minimum context legalPushAllowed reads: the caller
// (tenant + user), the system-admin flag and the narrow legal-upload flag,
// exactly as applyAuthSession writes them.
func ctxWithCaller(caller types.Caller, systemAdmin, legalUploader bool) context.Context {
	ctx := context.Background()
	ctx = context.WithValue(ctx, types.CallerContextKey, caller.Normalize())
	ctx = context.WithValue(ctx, types.UserIDContextKey, caller.UserID)
	ctx = context.WithValue(ctx, types.TenantIDContextKey, caller.TenantID)
	ctx = context.WithValue(ctx, types.SystemAdminContextKey, systemAdmin)
	ctx = context.WithValue(ctx, types.LegalUploaderContextKey, legalUploader)
	return ctx
}

func TestLegalPushAllowed(t *testing.T) {
	human := types.Caller{TenantID: types.PlatformTenantID, UserID: "u-operator"}

	t.Run("human with the narrow flag may push", func(t *testing.T) {
		if err := legalPushAllowed(ctxWithCaller(human, false, true)); err != nil {
			t.Fatalf("want allowed, got %v", err)
		}
	})

	t.Run("human without the flag is refused", func(t *testing.T) {
		if err := legalPushAllowed(ctxWithCaller(human, false, false)); err == nil {
			t.Fatal("want forbidden, got nil")
		}
	})

	t.Run("SuperAdmin keeps its access without the flag", func(t *testing.T) {
		if err := legalPushAllowed(ctxWithCaller(human, true, false)); err != nil {
			t.Fatalf("want allowed, got %v", err)
		}
	})

	t.Run("synthetic users never qualify through the flag", func(t *testing.T) {
		synthetic := types.Caller{TenantID: 7, UserID: "system-7"}
		if err := legalPushAllowed(ctxWithCaller(synthetic, false, true)); err == nil {
			t.Fatal("expected the synthetic user to be refused")
		}
	})

	t.Run("platform API key keeps its path", func(t *testing.T) {
		keyCaller := types.Caller{TenantID: types.PlatformTenantID, UserID: "api-key-1"}
		ctx := ctxWithCaller(keyCaller, false, false)
		ctx = types.WithTenantAPIKeyScope(ctx, types.TenantAPIKeyScope{ScopeType: types.APIKeyScopePlatform})
		if err := legalPushAllowed(ctx); err != nil {
			t.Fatalf("want allowed, got %v", err)
		}
	})

	t.Run("non-platform API key is still refused", func(t *testing.T) {
		keyCaller := types.Caller{TenantID: 7, UserID: "api-key-2"}
		ctx := ctxWithCaller(keyCaller, false, false)
		ctx = types.WithTenantAPIKeyScope(ctx, types.TenantAPIKeyScope{ScopeType: types.APIKeyScopeTenant})
		if err := legalPushAllowed(ctx); err == nil {
			t.Fatal("expected refusal for a tenant API key")
		}
	})

	t.Run("no caller at all is refused", func(t *testing.T) {
		if err := legalPushAllowed(context.Background()); err == nil {
			t.Fatal("expected refusal for an empty context")
		}
	})
}
