// Bootstrap-time hooks that run after the DI container is built but
// before the HTTP server starts listening. These are deliberately
// best-effort: any failure here only warns and does NOT abort startup.
// The reasoning is that an operator running with a misconfigured env
// var should still be able to bring the server up (and fix the issue
// from the running instance) rather than have a typo brick the deploy.
package main

import (
	"context"
	"os"
	"strings"

	"go.uber.org/dig"

	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

// bootstrapEnvVar is the env var that names the email of the user who
// may be promoted to system administrator when the deployment has no
// existing system administrators.
//
// Why an env var (vs a CLI subcommand)?
//   - Zero-friction in docker-compose / k8s deploys: set it once in the
//     manifest and the very first user account that signs up with that
//     email is auto-promoted, with no extra ops step.
//   - Idempotent: if the user is already a system admin, bootstrapping is
//     a no-op.
//   - Safe to leave set: once at least one system admin exists, the env
//     var stops granting privileges. That prevents a UI revoke from being
//     silently undone on the next restart.
const bootstrapEnvVar = "WEKNORA_BOOTSTRAP_SYSTEM_ADMIN_EMAIL"

// runStartupBootstrap consults the env and applies any one-shot
// bootstrap actions. Currently it only handles system-admin promotion;
// future bootstrap steps (default model seeding, etc.) can be added
// here as additional dig.Invoke calls.
func runStartupBootstrap(c *dig.Container) {
	ctx := context.Background()

	// Legacy hash repair for migration 000065 placeholder rows. Invoked each
	// startup but short-circuits with a cheap EXISTS once every row is
	// backfilled (no api_key decryption on the steady-state path).
	if err := c.Invoke(func(apiKeySvc interfaces.TenantAPIKeyService) {
		if n, err := apiKeySvc.BackfillMissingKeyHashes(ctx); err != nil {
			logger.Warnf(ctx, "[bootstrap] tenant api key hash backfill failed: %v", err)
		} else if n > 0 {
			logger.Infof(ctx, "[bootstrap] backfilled %d legacy tenant api key hash(es)", n)
		}
	}); err != nil {
		logger.Warnf(ctx, "[bootstrap] failed to resolve TenantAPIKeyService: %v", err)
	}

	email := strings.TrimSpace(os.Getenv(bootstrapEnvVar))
	if email == "" {
		return
	}
	// dig.Invoke resolves UserService from the container; if user
	// service registration is broken we want to know loudly, but still
	// not abort startup — bootstrap is best-effort.
	if err := c.Invoke(func(
		userSvc interfaces.UserService,
		tenantSvc interfaces.TenantService,
		memberSvc interfaces.TenantMemberService,
	) {
		if user := bootstrapSystemAdmin(ctx, userSvc, email); user != nil {
			attachPlatformTenant(ctx, tenantSvc, memberSvc, user)
		}
	}); err != nil {
		logger.Warnf(ctx, "[bootstrap] failed to resolve user/tenant/member services: %v", err)
	}
}

// bootstrapSystemAdmin promotes the user identified by `email` to system
// administrator only when the deployment currently has no system admins.
// The function is idempotent and non-fatal — it warns and returns nil on
// every error path. It returns the user when they are a system admin
// after the call (freshly promoted or already one) so the caller can
// finish the platform-tenant attachment.
//
// The bootstrap intentionally does NOT create a user when the email is
// not yet registered: account creation is a workflow with side effects
// (password hashing, tenant assignment, audit) that we don't want to
// short-circuit. Operators should sign up normally first, then set the
// env var on the next restart.
func bootstrapSystemAdmin(ctx context.Context, userSvc interfaces.UserService, email string) *types.User {
	user, err := userSvc.GetUserByEmail(ctx, email)
	if err != nil {
		// "not found" surfaces as an error in this codebase; treat it
		// gently — operators commonly set the var before the user has
		// signed up. The next restart after registration will succeed.
		logger.Warnf(ctx,
			"[bootstrap] %s=%s: user lookup failed (have they signed up yet?): %v",
			bootstrapEnvVar, email, err)
		return nil
	}
	if user == nil {
		logger.Warnf(ctx,
			"[bootstrap] %s=%s: no matching user (will retry on next restart)",
			bootstrapEnvVar, email)
		return nil
	}
	if user.IsSystemAdmin {
		logger.Infof(ctx,
			"[bootstrap] %s=%s: user %s is already a system admin (no-op)",
			bootstrapEnvVar, email, user.ID)
		return user
	}
	_, total, err := userSvc.ListSystemAdmins(ctx, 0, 1)
	if err != nil {
		logger.Warnf(ctx,
			"[bootstrap] %s=%s: cannot verify existing system admins, skipping promotion: %v",
			bootstrapEnvVar, email, err)
		return nil
	}
	if total > 0 {
		logger.Infof(ctx,
			"[bootstrap] %s=%s: %d system admin(s) already exist; not promoting user %s",
			bootstrapEnvVar, email, total, user.ID)
		return nil
	}
	user.IsSystemAdmin = true
	if err := userSvc.UpdateUser(ctx, user); err != nil {
		logger.Warnf(ctx,
			"[bootstrap] %s=%s: failed to promote user %s: %v",
			bootstrapEnvVar, email, user.ID, err)
		return nil
	}
	logger.Infof(ctx,
		"[bootstrap] promoted user %s (%s) to system admin via %s",
		user.ID, email, bootstrapEnvVar)
	return user
}

// attachPlatformTenant gives a bootstrapped SuperAdmin an Admin membership
// in the platform tenant (types.PlatformTenantID, seeded by migration
// 000150). The platform tenant is where public knowledge bases are created and fed, so
// a fresh install's first SuperAdmin lands there instead of in a personal
// workspace. Idempotent and best-effort: a missing tenant row (migration
// not yet applied) or any error only warns.
func attachPlatformTenant(
	ctx context.Context,
	tenantSvc interfaces.TenantService,
	memberSvc interfaces.TenantMemberService,
	user *types.User,
) {
	if tenantSvc == nil || memberSvc == nil || user == nil {
		return
	}
	tenant, err := tenantSvc.GetTenantByID(ctx, types.PlatformTenantID)
	if err != nil || tenant == nil {
		logger.Warnf(ctx,
			"[bootstrap] platform tenant %d not found, SuperAdmin %s not attached: %v",
			types.PlatformTenantID, user.ID, err)
		return
	}
	if _, err := memberSvc.EnsureAdmin(ctx, user.ID, types.PlatformTenantID); err != nil {
		logger.Warnf(ctx,
			"[bootstrap] failed to attach SuperAdmin %s to platform tenant %d: %v",
			user.ID, types.PlatformTenantID, err)
		return
	}
	logger.Infof(ctx,
		"[bootstrap] SuperAdmin %s is an admin of platform tenant %d (%s)",
		user.ID, types.PlatformTenantID, tenant.Name)
}
