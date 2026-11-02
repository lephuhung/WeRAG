package repository

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func openActingUserDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&types.Tenant{}, &types.User{}, &types.TenantMember{}))
	return db
}

var actingBase = time.Date(2026, 10, 1, 8, 0, 0, 0, time.UTC)

func seedUser(t *testing.T, db *gorm.DB, id string, homeTenant uint64, created time.Time, active bool) {
	t.Helper()
	u := &types.User{ID: id, Username: id, Email: id + "@x.vn", PasswordHash: "h", TenantID: homeTenant, IsActive: true, CreatedAt: created}
	require.NoError(t, db.Create(u).Error)
	if !active { // gorm default:true swallows a false on Create
		require.NoError(t, db.Model(&types.User{}).Where("id = ?", id).Update("is_active", false).Error)
	}
}

func seedMember(t *testing.T, db *gorm.DB, userID string, tenantID uint64, role types.TenantRole,
	status types.TenantMemberStatus, created time.Time) *types.TenantMember {
	t.Helper()
	m := &types.TenantMember{UserID: userID, TenantID: tenantID, Role: role, Status: status, JoinedAt: created, CreatedAt: created}
	require.NoError(t, db.Create(m).Error)
	return m
}

// Prefers the earliest active admin membership over an older home-pointer
// user (e.g. a guest whose users.tenant_id points at the org), ignores plain
// members, suspended and deleted memberships, and breaks created_at ties by
// membership id.
func TestGetUserByTenantIDPrefersEarliestActiveAdmin(t *testing.T) {
	db := openActingUserDB(t)
	repo := NewUserRepository(db)
	const tenant = 10003

	seedUser(t, db, "guest", tenant, actingBase, true)                 // home pointer only, oldest
	seedUser(t, db, "member", tenant, actingBase.Add(time.Hour), true) // plain member
	seedUser(t, db, "suspended", 0, actingBase.Add(time.Hour), true)
	seedUser(t, db, "deleted", 0, actingBase.Add(time.Hour), true)
	seedUser(t, db, "admin-b", 0, actingBase.Add(2*time.Hour), true)
	seedUser(t, db, "admin-a", 0, actingBase.Add(2*time.Hour), true)

	joined := actingBase.Add(3 * time.Hour)
	seedMember(t, db, "member", tenant, types.TenantRoleMember, types.TenantMemberStatusActive, actingBase)
	seedMember(t, db, "suspended", tenant, types.TenantRoleAdmin, types.TenantMemberStatusSuspended, actingBase)
	del := seedMember(t, db, "deleted", tenant, types.TenantRoleAdmin, types.TenantMemberStatusActive, actingBase)
	require.NoError(t, db.Delete(del).Error)
	// Same membership timestamp: the lower membership id (admin-b) wins.
	seedMember(t, db, "admin-b", tenant, types.TenantRoleAdmin, types.TenantMemberStatusActive, joined)
	seedMember(t, db, "admin-a", tenant, types.TenantRoleOwner, types.TenantMemberStatusActive, joined)

	for i := 0; i < 5; i++ {
		u, err := repo.GetUserByTenantID(context.Background(), tenant)
		require.NoError(t, err)
		require.Equal(t, "admin-b", u.ID)
	}
}

// A locked chosen admin is still returned (the caller refuses the key); the
// key never moves to the next admin.
func TestGetUserByTenantIDReturnsLockedAdmin(t *testing.T) {
	db := openActingUserDB(t)
	repo := NewUserRepository(db)
	const tenant = 7

	seedUser(t, db, "first-admin", 0, actingBase, false)
	seedUser(t, db, "second-admin", 0, actingBase, true)
	seedMember(t, db, "first-admin", tenant, types.TenantRoleAdmin, types.TenantMemberStatusActive, actingBase)
	seedMember(t, db, "second-admin", tenant, types.TenantRoleAdmin, types.TenantMemberStatusActive, actingBase.Add(time.Minute))

	u, err := repo.GetUserByTenantID(context.Background(), tenant)
	require.NoError(t, err)
	require.Equal(t, "first-admin", u.ID)
	require.False(t, u.IsActive)
}

// No admin member: the earliest home-pointer user, ties broken by id.
func TestGetUserByTenantIDFallsBackToHomeUserDeterministically(t *testing.T) {
	db := openActingUserDB(t)
	repo := NewUserRepository(db)
	const tenant = 9

	seedUser(t, db, "user-z", tenant, actingBase, true)
	seedUser(t, db, "user-m", tenant, actingBase, true)
	seedUser(t, db, "later", tenant, actingBase.Add(time.Second), true)
	seedUser(t, db, "other-org", 8, actingBase.Add(-time.Hour), true)

	for i := 0; i < 5; i++ {
		u, err := repo.GetUserByTenantID(context.Background(), tenant)
		require.NoError(t, err)
		require.Equal(t, "user-m", u.ID)
	}

	_, err := repo.GetUserByTenantID(context.Background(), 404)
	require.True(t, errors.Is(err, ErrUserNotFound), err)
}
