package types

import (
	"context"
	"errors"
	"strings"
	"time"
)

// Tasks a document scope may name: what the conversation is doing with the
// scoped documents ("" = not said).
const (
	DocumentScopeTaskFormat   = "format"
	DocumentScopeTaskSpelling = "spelling"
	DocumentScopeTaskSummary  = "summary"
	DocumentScopeTaskLookup   = "lookup"
	DocumentScopeTaskCompare  = "compare"
	DocumentScopeTaskEdit     = "edit"
)

// Who set a scope: the user (the scope chip, PUT …/documents/scope) or the
// document router of a turn that named no document.
const (
	DocumentScopeSetByUser   = "user"
	DocumentScopeSetByRouter = "router"
)

// DocumentScopeMaxSections bounds the sections of one scope.
const DocumentScopeMaxSections = 12

// DocumentScope is the part of a document-assistant session a conversation
// is about: which documents (workspace IDs), optionally which sections of
// them (index ranges in the document's unit, as on its card), and the task.
// It is kept per session so a turn that names no document reads the scoped
// part instead of guessing again; an @-mention or a selection overrides it
// for that turn.
type DocumentScope struct {
	DocumentIDs []string               `json:"document_ids"`
	Sections    []DocumentScopeSection `json:"sections,omitempty"`
	Task        string                 `json:"task,omitempty"`
	SetBy       string                 `json:"set_by"`
	// Query is the request the scope was chosen for (router scopes).
	Query string    `json:"query,omitempty"`
	At    time.Time `json:"at"`
}

// DocumentScopeSection is one section of a scoped document: the inclusive
// index range From..To and its title.
type DocumentScopeSection struct {
	DocumentID string `json:"document_id"`
	From       int    `json:"from"`
	To         int    `json:"to"`
	Title      string `json:"title,omitempty"`
}

// ValidDocumentScopeTask reports whether task is one a scope may name.
func ValidDocumentScopeTask(task string) bool {
	switch task {
	case "", DocumentScopeTaskFormat, DocumentScopeTaskSpelling, DocumentScopeTaskSummary,
		DocumentScopeTaskLookup, DocumentScopeTaskCompare, DocumentScopeTaskEdit:
		return true
	}
	return false
}

// Normalize trims and de-duplicates the scope and checks it: at least one
// document, a known task and setter, every section in a scoped document
// with From ≤ To.
func (s *DocumentScope) Normalize() error {
	if s == nil {
		return errors.New("empty scope")
	}
	seen := map[string]bool{}
	ids := s.DocumentIDs[:0:0]
	for _, id := range s.DocumentIDs {
		if id = strings.TrimSpace(id); id != "" && !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return errors.New("a scope names at least one document")
	}
	s.DocumentIDs = ids
	s.Task = strings.ToLower(strings.TrimSpace(s.Task))
	if !ValidDocumentScopeTask(s.Task) {
		return errors.New("unknown task " + s.Task)
	}
	if s.SetBy != DocumentScopeSetByUser && s.SetBy != DocumentScopeSetByRouter {
		return errors.New("a scope is set by the user or the router")
	}
	if len(s.Sections) > DocumentScopeMaxSections {
		return errors.New("too many sections")
	}
	for i := range s.Sections {
		sec := &s.Sections[i]
		sec.DocumentID = strings.TrimSpace(sec.DocumentID)
		sec.Title = truncateRunes(collapseWhitespace(sec.Title), documentProfileMaxTitleRunes)
		if !seen[sec.DocumentID] {
			return errors.New("a section must belong to a scoped document")
		}
		if sec.From < 0 || sec.To < sec.From {
			return errors.New("a section range is from ≤ to")
		}
	}
	s.Query = truncateRunes(strings.TrimSpace(s.Query), 500)
	return nil
}

// Includes reports whether the scope names the document.
func (s *DocumentScope) Includes(documentID string) bool {
	if s == nil {
		return false
	}
	for _, id := range s.DocumentIDs {
		if id == documentID {
			return true
		}
	}
	return false
}

// SectionsOf returns the scope's sections of one document.
func (s *DocumentScope) SectionsOf(documentID string) []DocumentScopeSection {
	if s == nil {
		return nil
	}
	var out []DocumentScopeSection
	for _, sec := range s.Sections {
		if sec.DocumentID == documentID {
			out = append(out, sec)
		}
	}
	return out
}

// documentScopeContextKey carries the scope applied to the current turn
// from the router to the prompt builder.
const documentScopeContextKey ContextKey = "DocumentScope"

// WithDocumentScope returns ctx carrying the turn's applied scope; nil
// leaves ctx unchanged.
func WithDocumentScope(ctx context.Context, scope *DocumentScope) context.Context {
	if scope == nil || len(scope.DocumentIDs) == 0 {
		return ctx
	}
	return context.WithValue(ctx, documentScopeContextKey, scope)
}

// DocumentScopeFromContext returns the scope applied to the current turn,
// or nil.
func DocumentScopeFromContext(ctx context.Context) *DocumentScope {
	if ctx == nil {
		return nil
	}
	s, _ := ctx.Value(documentScopeContextKey).(*DocumentScope)
	return s
}
