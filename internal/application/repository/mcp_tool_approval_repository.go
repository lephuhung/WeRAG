package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// MCPToolApprovalRepository implements interfaces.MCPToolApprovalRepository.
type MCPToolApprovalRepository struct {
	db *gorm.DB
}

// NewMCPToolApprovalRepository creates a repository backed by GORM.
func NewMCPToolApprovalRepository(db *gorm.DB) interfaces.MCPToolApprovalRepository {
	return &MCPToolApprovalRepository{db: db}
}

// ListByService returns the approval rows in force for an MCP service in a
// tenant: its own rows, plus the configuration profile's rows for tools the
// tenant has no row for (may be empty).
func (r *MCPToolApprovalRepository) ListByService(ctx context.Context, tenantID uint64, serviceID string) ([]*types.MCPToolApproval, error) {
	var rows []*types.MCPToolApproval
	err := r.db.WithContext(ctx).
		Where("service_id = ?", serviceID).
		Where(configScopeSQL("tenant_id"), configScopeArgs(tenantID)...).
		Order("tool_name ASC").
		Find(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("list mcp tool approvals: %w", err)
	}
	own := make(map[string]bool, len(rows))
	for _, row := range rows {
		if row.TenantID == tenantID {
			own[row.ToolName] = true
		}
	}
	out := rows[:0]
	for _, row := range rows {
		if row.TenantID != tenantID && own[row.ToolName] {
			continue
		}
		out = append(out, row)
	}
	return out, nil
}

// policyRow returns the policy row in force for one tool: the tenant's own,
// else its configuration profile's (the SuperAdmin sets policy there), or
// nil when neither has one.
func (r *MCPToolApprovalRepository) policyRow(ctx context.Context, tenantID uint64, serviceID, toolName string) (*types.MCPToolApproval, error) {
	var rows []*types.MCPToolApproval
	err := r.db.WithContext(ctx).
		Where("service_id = ? AND tool_name = ?", serviceID, toolName).
		Where(configScopeSQL("tenant_id"), configScopeArgs(tenantID)...).
		Find(&rows).Error
	if err != nil {
		return nil, err
	}
	var picked *types.MCPToolApproval
	for _, row := range rows {
		if picked == nil || row.TenantID == tenantID {
			picked = row
		}
	}
	return picked, nil
}

// IsRequired returns true when the policy in force has require_approval = true.
func (r *MCPToolApprovalRepository) IsRequired(ctx context.Context, tenantID uint64, serviceID, toolName string) (bool, error) {
	row, err := r.policyRow(ctx, tenantID, serviceID, toolName)
	if err != nil {
		return false, fmt.Errorf("get mcp tool approval: %w", err)
	}
	return row != nil && row.RequireApproval, nil
}

// IsEnabled returns true when a tool has no stored policy row. This preserves
// the historical behaviour for tools discovered before per-tool settings.
func (r *MCPToolApprovalRepository) IsEnabled(ctx context.Context, tenantID uint64, serviceID, toolName string) (bool, error) {
	row, err := r.policyRow(ctx, tenantID, serviceID, toolName)
	if err != nil {
		return false, fmt.Errorf("get mcp tool enabled state: %w", err)
	}
	return row == nil || row.Enabled, nil
}

// UpsertPolicy creates or updates only the supplied policy columns.
// Concurrent patches of different fields cannot clobber each other because
// ON CONFLICT updates omit unchanged columns. A first insert uses
// require_approval=false and enabled=true for any field the patch omits.
func (r *MCPToolApprovalRepository) UpsertPolicy(
	ctx context.Context, tenantID uint64, serviceID, toolName string, patch types.MCPToolPolicyPatch,
) error {
	if serviceID == "" || toolName == "" {
		return errors.New("service_id and tool_name are required")
	}
	if patch.RequireApproval == nil && patch.Enabled == nil {
		return errors.New("require_approval or enabled is required")
	}

	now := time.Now()
	requireApproval := false
	if patch.RequireApproval != nil {
		requireApproval = *patch.RequireApproval
	}
	enabled := true
	if patch.Enabled != nil {
		enabled = *patch.Enabled
	}

	updates := map[string]interface{}{"updated_at": now}
	if patch.RequireApproval != nil {
		updates["require_approval"] = *patch.RequireApproval
	}
	if patch.Enabled != nil {
		updates["enabled"] = *patch.Enabled
	}

	row := map[string]interface{}{
		"id":               uuid.New().String(),
		"tenant_id":        tenantID,
		"service_id":       serviceID,
		"tool_name":        toolName,
		"require_approval": requireApproval,
		"enabled":          enabled,
		"created_at":       now,
		"updated_at":       now,
	}
	// Create via map so enabled=false is persisted. GORM struct inserts omit
	// bool zero values when the field has a default tag.
	err := r.db.WithContext(ctx).Model(&types.MCPToolApproval{}).Clauses(clause.OnConflict{
		Columns: []clause.Column{
			{Name: "tenant_id"},
			{Name: "service_id"},
			{Name: "tool_name"},
		},
		DoUpdates: clause.Assignments(updates),
	}).Create(row).Error
	if err != nil {
		return fmt.Errorf("upsert mcp tool policy: %w", err)
	}
	return nil
}
