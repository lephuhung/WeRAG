package service

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/jackc/pgx/v5/pgconn"

	werrors "github.com/Tencent/WeKnora/internal/errors"
)

// TenantNameMaxLength matches tenants.name VARCHAR(255) (characters).
const TenantNameMaxLength = 255

// tenantWorkspaceNameIndex is the partial unique index from migration 000160
// (lower(btrim(name)) among live workspace tenants). A violation of it is the
// race-loser path of the service-level duplicate check.
const tenantWorkspaceNameIndex = "idx_tenants_workspace_name_unique"

// Messages returned to the client; the web UI maps the error codes
// (ErrTenantAlreadyExists, ErrTenantNameRequired, ErrTenantNameTooLong) to
// i18n strings.
const (
	tenantNameRequiredMessage = "organization name is required"
	tenantNameTakenMessage    = "an organization with this name already exists"
)

// NormalizeTenantName trims an organization name and collapses inner runs of
// whitespace to single spaces.
func NormalizeTenantName(name string) string {
	return strings.Join(strings.Fields(name), " ")
}

// tenantNameKey is the comparison key for uniqueness: normalized and
// lower-cased (Unicode-aware, so "SỞ TÀI CHÍNH" equals "Sở Tài Chính").
func tenantNameKey(name string) string {
	return strings.ToLower(NormalizeTenantName(name))
}

func newTenantNameTakenError() error {
	return &werrors.AppError{
		Code:     werrors.ErrTenantAlreadyExists,
		Message:  tenantNameTakenMessage,
		HTTPCode: http.StatusConflict,
	}
}

func newTenantNameInvalidError(code werrors.ErrorCode, message string) error {
	return &werrors.AppError{Code: code, Message: message, HTTPCode: http.StatusBadRequest}
}

// isTenantNameUniqueViolation recognises a write rejected by the workspace
// name index.
func isTenantNameUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == "23505" && pgErr.ConstraintName == tenantWorkspaceNameIndex
	}
	return isUniqueViolation(err) && strings.Contains(err.Error(), tenantWorkspaceNameIndex)
}

// ValidateTenantName normalizes an organization name and checks it is present,
// fits the column and is not used by another live organization (case
// insensitive). excludeID is the organization being renamed (0 on create), so
// keeping its own name is accepted. Returns the normalized name; errors are
// AppErrors (400 for a missing/too long name, 409 for a duplicate).
func (s *tenantService) ValidateTenantName(ctx context.Context, name string, excludeID uint64) (string, error) {
	normalized := NormalizeTenantName(name)
	if normalized == "" {
		return "", newTenantNameInvalidError(werrors.ErrTenantNameRequired, tenantNameRequiredMessage)
	}
	if utf8.RuneCountInString(normalized) > TenantNameMaxLength {
		return "", newTenantNameInvalidError(werrors.ErrTenantNameTooLong,
			fmt.Sprintf("organization name must be at most %d characters", TenantNameMaxLength))
	}
	tenants, err := s.repo.ListTenants(ctx)
	if err != nil {
		return "", err
	}
	key := strings.ToLower(normalized)
	for _, t := range tenants {
		if t == nil || t.ID == excludeID || t.IsHiddenTenant() {
			continue
		}
		if tenantNameKey(t.Name) == key {
			return "", newTenantNameTakenError()
		}
	}
	return normalized, nil
}

// mapTenantWriteError turns a lost race on the workspace name index into the
// same 409 the pre-check returns.
func mapTenantWriteError(err error) error {
	if isTenantNameUniqueViolation(err) {
		return newTenantNameTakenError()
	}
	return err
}
