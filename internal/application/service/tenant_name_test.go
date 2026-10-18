package service

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/application/repository"
	werrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func newTenantNameTestService(t *testing.T) (*tenantService, *gorm.DB) {
	t.Helper()
	t.Setenv("STORAGE_TYPE", "local")
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&types.Tenant{}, &types.StorageBackend{}))
	svc := NewTenantService(repository.NewTenantRepository(db), repository.NewStorageBackendRepository(db))
	return svc.(*tenantService), db
}

func requireAppErrorStatus(t *testing.T, err error, status int) {
	t.Helper()
	require.Error(t, err)
	appErr, ok := werrors.IsAppError(err)
	require.True(t, ok, "want AppError, got %T: %v", err, err)
	require.Equal(t, status, appErr.HTTPCode, appErr.Message)
}

func requireAppErrorCode(t *testing.T, err error, code werrors.ErrorCode) {
	t.Helper()
	appErr, ok := werrors.IsAppError(err)
	require.True(t, ok, "want AppError, got %T: %v", err, err)
	require.Equal(t, code, appErr.Code, appErr.Message)
}

func TestNormalizeTenantName(t *testing.T) {
	require.Equal(t, "UBND xã Phú Vang", NormalizeTenantName("  UBND \t xã   Phú\nVang  "))
	require.Equal(t, "", NormalizeTenantName(" \t "))
}

func TestCreateTenantValidatesName(t *testing.T) {
	ctx := context.Background()
	svc, _ := newTenantNameTestService(t)

	created, err := svc.CreateTenant(ctx, &types.Tenant{Name: "  Sở  Tài chính "})
	require.NoError(t, err)
	require.Equal(t, "Sở Tài chính", created.Name)

	// Case-insensitive (Unicode) and whitespace-insensitive duplicate.
	_, err = svc.CreateTenant(ctx, &types.Tenant{Name: "SỞ TÀI   CHÍNH"})
	requireAppErrorStatus(t, err, http.StatusConflict)
	require.Contains(t, err.Error(), "an organization with this name already exists")
	requireAppErrorCode(t, err, werrors.ErrTenantAlreadyExists)

	_, err = svc.CreateTenant(ctx, &types.Tenant{Name: "   "})
	requireAppErrorStatus(t, err, http.StatusBadRequest)
	requireAppErrorCode(t, err, werrors.ErrTenantNameRequired)

	_, err = svc.CreateTenant(ctx, &types.Tenant{Name: strings.Repeat("ạ", TenantNameMaxLength+1)})
	requireAppErrorStatus(t, err, http.StatusBadRequest)
	requireAppErrorCode(t, err, werrors.ErrTenantNameTooLong)

	// Exactly the column width (in characters, not bytes) is accepted.
	_, err = svc.CreateTenant(ctx, &types.Tenant{Name: strings.Repeat("ạ", TenantNameMaxLength)})
	require.NoError(t, err)
}

func TestCreateTenantIgnoresDeletedAndConfigProfileNames(t *testing.T) {
	ctx := context.Background()
	svc, db := newTenantNameTestService(t)

	old, err := svc.CreateTenant(ctx, &types.Tenant{Name: "UBND xã Phú Vang"})
	require.NoError(t, err)
	require.NoError(t, db.Delete(&types.Tenant{}, old.ID).Error)
	require.NoError(t, db.Create(&types.Tenant{Name: "Cấu hình chung", Kind: types.TenantKindConfigProfile}).Error)

	_, err = svc.CreateTenant(ctx, &types.Tenant{Name: "UBND xã Phú Vang"})
	require.NoError(t, err, "a deleted organization's name is free again")
	_, err = svc.CreateTenant(ctx, &types.Tenant{Name: "cấu hình chung"})
	require.NoError(t, err, "configuration profiles do not reserve names")
}

func TestUpdateTenantRenameValidatesName(t *testing.T) {
	ctx := context.Background()
	svc, _ := newTenantNameTestService(t)

	a, err := svc.CreateTenant(ctx, &types.Tenant{Name: "Sở Tài chính"})
	require.NoError(t, err)
	b, err := svc.CreateTenant(ctx, &types.Tenant{Name: "Sở Y tế"})
	require.NoError(t, err)

	_, err = svc.UpdateTenant(ctx, &types.Tenant{ID: b.ID, Name: "sở tài chính"})
	requireAppErrorStatus(t, err, http.StatusConflict)

	// Renaming to its own name (also re-cased) is fine.
	_, err = svc.UpdateTenant(ctx, &types.Tenant{ID: a.ID, Name: "Sở Tài chính"})
	require.NoError(t, err)
	renamed, err := svc.UpdateTenant(ctx, &types.Tenant{ID: a.ID, Name: " SỞ TÀI  CHÍNH "})
	require.NoError(t, err)
	require.Equal(t, "SỞ TÀI CHÍNH", renamed.Name)

	// A partial update without a name leaves it alone.
	_, err = svc.UpdateTenant(ctx, &types.Tenant{ID: b.ID, Description: "y tế"})
	require.NoError(t, err)
}

// Legacy rows that already share a name must not block unrelated saves of
// those organizations (configuration updates pass the unchanged name back).
func TestUpdateTenantUnchangedLegacyDuplicateNameIsAccepted(t *testing.T) {
	ctx := context.Background()
	svc, db := newTenantNameTestService(t)

	first := &types.Tenant{Name: "Tổ chức", Kind: types.TenantKindWorkspace}
	second := &types.Tenant{Name: "tổ chức", Kind: types.TenantKindWorkspace}
	require.NoError(t, db.Create(first).Error)
	require.NoError(t, db.Create(second).Error)

	second.Description = "cập nhật cấu hình"
	_, err := svc.UpdateTenant(ctx, second)
	require.NoError(t, err)
}

// The DB index (migration 000160) is the race guard; a write it rejects is
// reported as the same 409. SQLite reports an expression index by name.
func TestTenantNameIndexViolationMapsToConflict(t *testing.T) {
	ctx := context.Background()
	svc, db := newTenantNameTestService(t)
	require.NoError(t, db.Exec(`CREATE UNIQUE INDEX `+tenantWorkspaceNameIndex+
		` ON tenants (lower(trim(name))) WHERE deleted_at IS NULL AND kind = 'workspace'`).Error)

	require.NoError(t, db.Create(&types.Tenant{Name: "Sở Nội vụ", Kind: types.TenantKindWorkspace}).Error)
	err := svc.repo.CreateTenant(ctx, &types.Tenant{Name: "sở nội vụ", Kind: types.TenantKindWorkspace})
	require.Error(t, err)
	requireAppErrorStatus(t, mapTenantWriteError(err), http.StatusConflict)

	pgErr := &fakePGError{msg: `ERROR: duplicate key value violates unique constraint "` +
		tenantWorkspaceNameIndex + `" (SQLSTATE 23505)`}
	requireAppErrorStatus(t, mapTenantWriteError(pgErr), http.StatusConflict)

	typed := fmt.Errorf("create tenant: %w", &pgconn.PgError{Code: "23505", ConstraintName: tenantWorkspaceNameIndex})
	requireAppErrorStatus(t, mapTenantWriteError(typed), http.StatusConflict)
	otherTyped := &pgconn.PgError{Code: "23505", ConstraintName: "tenants_pkey"}
	require.Same(t, error(otherTyped), mapTenantWriteError(otherTyped))

	other := &fakePGError{msg: `ERROR: duplicate key value violates unique constraint "tenants_pkey" (SQLSTATE 23505)`}
	require.Same(t, other, mapTenantWriteError(other))
}

type fakePGError struct{ msg string }

func (e *fakePGError) Error() string { return e.msg }
