package service

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"

	"github.com/Tencent/WeKnora/internal/agent/tools"
	apperrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

// legalBasisTimeout bounds the catalogue lookups of one legal basis check;
// the editor save before it has its own wait (formatCheckSaveWait).
const legalBasisTimeout = 30 * time.Second

// SetLegalBasis wires the legal catalogue the legal basis check reads; db
// resolves laws named by title (ResolveLegalDocumentName).
func (p *DocumentFormatPrecheck) SetLegalBasis(catalog interfaces.LegalCatalogRepository, db *gorm.DB) {
	p.legalCatalog, p.legalDB = catalog, db
}

// LegalBasisCheck checks the documents an open working document cites
// against the legal catalogue (tools.CheckLegalBasis), as the user asked
// from the editor. No model is called and nothing is stored or changed:
// the report carries the underlines for the editor to show on request.
// An ONLYOFFICE editor is saved first, as for the format check; when that
// save fails the last saved version is checked and the report says so.
func (p *DocumentFormatPrecheck) LegalBasisCheck(ctx context.Context, tenantID uint64, sessionID, documentID string) (*tools.LegalBasisReport, error) {
	if p == nil || p.workspaces == nil || !p.workspaces.DocumentsEnabled() {
		return nil, errors.New("documents are disabled")
	}
	if p.legalCatalog == nil {
		return nil, apperrors.NewServiceUnavailableError("the legal catalogue is not available")
	}
	ws, err := p.workspaces.Get(ctx, tenantID, sessionID, documentID)
	if err != nil {
		return nil, err
	}
	// the format check's rule: an open working document only
	if !ws.IsTarget() || ws.Status != types.DocumentWorkspaceStatusOpen {
		return nil, apperrors.NewBadRequestError("only an open working document can be checked")
	}
	ctx = context.WithValue(ctx, types.TenantIDContextKey, tenantID)
	unsaved := false
	if !ws.IsWordAddin() {
		const label = "ai: rà soát căn cứ pháp lý"
		_, saved, err := p.workspaces.SnapshotSaved(ctx, tenantID, sessionID, ws.ID, label, types.DocumentRevisionSourceAI, formatCheckSaveWait)
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			logger.Warnf(ctx, "[LegalBasis] save before the check failed, the stored version is checked: %v", err)
		}
		unsaved = err != nil || !saved
	}
	// the lookups get their own budget, after the save
	ctx, cancel := context.WithTimeout(ctx, legalBasisTimeout)
	defer cancel()
	paras, read, err := tools.ReadLegalBasisParagraphs(ctx, p.workspaces, sessionID, ws.ID)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	report, err := tools.CheckLegalBasis(ctx, paras, p.legalCatalog, legalBasisNames(p.legalDB, now), now)
	if err != nil {
		return nil, err
	}
	report.DocumentID, report.Revision, report.Unsaved = read.ID, read.Revision, unsaved
	return report, nil
}

// legalBasisNames resolves a law named with a year as of now: the
// catalogue document of that title and year. A guess from a shorter name
// (Trimmed) is offered as a candidate, never as the answer.
func legalBasisNames(db *gorm.DB, now time.Time) tools.LegalNameResolver {
	if db == nil {
		return nil
	}
	return func(ctx context.Context, name, year string) (tools.LegalNameResolution, error) {
		m, err := ResolveLegalDocumentName(ctx, db, LegalNameQuery{Name: name, Year: year, At: now})
		if err != nil {
			return tools.LegalNameResolution{}, err
		}
		switch {
		case m.Resolution == LegalNameUnique && m.Chosen != nil && !m.Trimmed:
			return tools.LegalNameResolution{Chosen: m.Chosen}, nil
		case m.Resolution == LegalNameUnique && m.Chosen != nil:
			return tools.LegalNameResolution{Candidates: []*types.LegalDocument{m.Chosen}, Guess: true}, nil
		}
		var out tools.LegalNameResolution
		for _, c := range m.Candidates {
			out.Candidates = append(out.Candidates, c.Document)
		}
		return out, nil
	}
}
