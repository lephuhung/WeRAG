package service

import (
	"context"
	"strings"

	"github.com/Tencent/WeKnora/internal/types"
)

// sessionHasSourceDocuments reports whether the session holds a source
// document (a chat upload of the document assistant), which
// read_document_outline reads even when no target is open.
func (s *agentService) sessionHasSourceDocuments(ctx context.Context, sessionID string) bool {
	if s.documentWorkspaces == nil || !s.documentWorkspaces.DocumentsEnabled() || strings.TrimSpace(sessionID) == "" {
		return false
	}
	tenantID, ok := types.TenantIDFromContext(ctx)
	if !ok || tenantID == 0 {
		return false
	}
	docs, err := s.documentWorkspaces.List(ctx, tenantID, sessionID)
	if err != nil {
		return false
	}
	for _, d := range docs {
		if d.IsSource() {
			return true
		}
	}
	return false
}
