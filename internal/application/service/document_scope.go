package service

import (
	"context"
	"strings"
	"time"

	"github.com/Tencent/WeKnora/internal/agent/tools"
	apperrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

// DocumentScopeService reads and sets the document scope of a
// document-assistant session: the documents (and sections) a turn that
// names none is about. The router sets it during a turn (see
// tools.ApplyDocumentScope); the user sets or clears it here, and a scope
// set here wins over the router until cleared.
type DocumentScopeService struct {
	workspaces interfaces.DocumentWorkspaceService
}

// NewDocumentScopeService builds the service over the session documents.
func NewDocumentScopeService(workspaces interfaces.DocumentWorkspaceService) *DocumentScopeService {
	return &DocumentScopeService{workspaces: workspaces}
}

// Get returns the session's scope with only the documents it still holds,
// or nil.
func (s *DocumentScopeService) Get(ctx context.Context, tenantID uint64, sessionID string, docs []*types.DocumentWorkspace) *types.DocumentScope {
	scope := tools.SessionDocumentScope(ctx, sessionID)
	if scope == nil {
		return nil
	}
	if docs == nil && s.workspaces != nil {
		var err error
		if docs, err = s.workspaces.List(ctx, tenantID, sessionID); err != nil {
			return nil
		}
	}
	have := map[string]bool{}
	for _, d := range docs {
		have[d.ID] = true
	}
	out := *scope
	out.DocumentIDs, out.Sections = nil, nil
	for _, id := range scope.DocumentIDs {
		if have[id] {
			out.DocumentIDs = append(out.DocumentIDs, id)
		}
	}
	for _, sec := range scope.Sections {
		if have[sec.DocumentID] {
			out.Sections = append(out.Sections, sec)
		}
	}
	if len(out.DocumentIDs) == 0 {
		return nil
	}
	return &out
}

// Set stores a user scope. Documents (and the document of each section)
// may be given by workspace ID or handle (vb2); each must be a document of
// the session. The stored scope is returned, and the documents and task
// count as answered for the clarification gate.
func (s *DocumentScopeService) Set(ctx context.Context, tenantID uint64, sessionID string, in *types.DocumentScope) (*types.DocumentScope, error) {
	if in == nil {
		return nil, apperrors.NewBadRequestError("scope is required")
	}
	docs, err := s.workspaces.List(ctx, tenantID, sessionID)
	if err != nil {
		return nil, err
	}
	resolve := func(ref string) string {
		ref = strings.TrimSpace(ref)
		for _, d := range docs {
			if d.ID == ref || strings.EqualFold(d.Handle(), ref) {
				return d.ID
			}
		}
		return ""
	}
	scope := &types.DocumentScope{Task: in.Task, SetBy: types.DocumentScopeSetByUser, At: time.Now()}
	for _, ref := range in.DocumentIDs {
		id := resolve(ref)
		if id == "" {
			return nil, apperrors.NewBadRequestError("the session has no document " + ref)
		}
		scope.DocumentIDs = append(scope.DocumentIDs, id)
	}
	for _, sec := range in.Sections {
		id := resolve(sec.DocumentID)
		if id == "" {
			return nil, apperrors.NewBadRequestError("the session has no document " + sec.DocumentID)
		}
		sec.DocumentID = id
		scope.Sections = append(scope.Sections, sec)
	}
	if err := scope.Normalize(); err != nil {
		return nil, apperrors.NewBadRequestError(err.Error())
	}
	tools.SetSessionDocumentScope(ctx, sessionID, scope)
	// a scope the user chose answers the long-document question for its
	// documents and task (see tools.DocumentScopeClarification)
	tools.MarkScopeClarificationAnswered(ctx, sessionID, scope.DocumentIDs, scope.Task)
	return scope, nil
}

// Clear removes the session's scope (user or router).
func (s *DocumentScopeService) Clear(ctx context.Context, sessionID string) {
	tools.ClearSessionDocumentScope(ctx, sessionID)
}
