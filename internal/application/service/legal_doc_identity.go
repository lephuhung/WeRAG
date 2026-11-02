package service

import (
	"context"

	"github.com/Tencent/WeKnora/internal/agent/tools"
	"github.com/Tencent/WeKnora/internal/logger"
)

// legalDocIdentity reads the legal identity (số hiệu, type, issue date,
// status) the vbpl importer stores in knowledges.metadata, for the given
// knowledge ids; documents without a số hiệu are left out. A calculation
// tool checks the basis of its legal figures with it.
func (s *agentService) legalDocIdentity(ctx context.Context, knowledgeIDs []string) map[string]tools.LegalDocIdentity {
	out := map[string]tools.LegalDocIdentity{}
	if s.db == nil || len(knowledgeIDs) == 0 {
		return out
	}
	var rows []struct {
		ID, Number, Type, IssueDate, Status string
	}
	err := s.db.WithContext(ctx).Raw(`SELECT id,
			COALESCE(metadata->>'doc_number', '') AS number,
			COALESCE(metadata->>'doc_type', '') AS type,
			COALESCE(metadata->>'issue_date', '') AS issue_date,
			COALESCE(metadata->>'eff_status', '') AS status
		FROM knowledges WHERE id IN ? AND deleted_at IS NULL`, knowledgeIDs).Scan(&rows).Error
	if err != nil {
		logger.Warnf(ctx, "[LegalDocIdentity] lookup failed: %v", err)
		return out
	}
	for _, r := range rows {
		if r.Number != "" {
			out[r.ID] = tools.LegalDocIdentity{Number: r.Number, Type: r.Type, IssueDate: r.IssueDate, Status: r.Status}
		}
	}
	return out
}
