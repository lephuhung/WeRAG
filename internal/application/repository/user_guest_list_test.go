package repository

import (
	"context"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// Guests are the active non-SuperAdmin accounts without an active
// membership of a live, named tenant.
func TestUserRepositoryListGuestUsers(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:user_guest_list?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&types.Tenant{}, &types.User{}, &types.TenantMember{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	ctx := context.Background()
	repo := NewUserRepository(db)
	live := &types.Tenant{ID: 20001, Name: "Sở Tư pháp"}
	gone := &types.Tenant{ID: 20002, Name: "Đã xoá"}
	for _, tn := range []*types.Tenant{live, gone} {
		if err := db.Create(tn).Error; err != nil {
			t.Fatalf("create tenant: %v", err)
		}
	}
	if err := db.Delete(gone).Error; err != nil {
		t.Fatalf("delete tenant: %v", err)
	}
	users := []*types.User{
		{ID: "u-guest", Username: "guest", Email: "g@x", IsActive: true},
		{ID: "u-member", Username: "member", Email: "m@x", IsActive: true},
		{ID: "u-stale", Username: "stale", Email: "s@x", IsActive: true},
		{ID: "u-suspended", Username: "suspended", Email: "p@x", IsActive: true},
		{ID: "u-admin", Username: "admin", Email: "a@x", IsActive: true, IsSystemAdmin: true},
		{ID: "u-off", Username: "off", Email: "o@x", IsActive: true},
	}
	for _, u := range users {
		if err := repo.CreateUser(ctx, u); err != nil {
			t.Fatalf("create user: %v", err)
		}
	}
	// gorm skips a false bool on create
	if err := db.Model(&types.User{}).Where("id = ?", "u-off").Update("is_active", false).Error; err != nil {
		t.Fatalf("deactivate: %v", err)
	}
	now := time.Now()
	for _, m := range []*types.TenantMember{
		{UserID: "u-member", TenantID: live.ID, Role: "contributor", Status: types.TenantMemberStatusActive, JoinedAt: now},
		{UserID: "u-stale", TenantID: gone.ID, Role: "contributor", Status: types.TenantMemberStatusActive, JoinedAt: now},
		{UserID: "u-suspended", TenantID: live.ID, Role: "contributor", Status: "suspended", JoinedAt: now},
	} {
		if err := db.Create(m).Error; err != nil {
			t.Fatalf("create member: %v", err)
		}
	}

	got, err := repo.ListGuestUsers(ctx)
	if err != nil {
		t.Fatalf("ListGuestUsers: %v", err)
	}
	ids := map[string]bool{}
	for _, u := range got {
		ids[u.ID] = true
	}
	want := map[string]bool{"u-guest": true, "u-stale": true, "u-suspended": true}
	if len(ids) != len(want) {
		t.Fatalf("guests = %v, want %v", ids, want)
	}
	for id := range want {
		if !ids[id] {
			t.Fatalf("guests = %v, want %v", ids, want)
		}
	}
}
