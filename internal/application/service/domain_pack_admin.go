package service

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/Tencent/WeKnora/internal/agent/domainpacks"
	apperrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/vietnamese_legal"
)

// agentLookup finds the agents using a pack (DomainPackAgentRepository).
type agentLookup interface {
	ListAgentsUsingPack(ctx context.Context, name string) ([]types.DomainPackAgent, error)
}

// List summarises every shipped pack for the SuperAdmin page.
func (r *DomainPackReconciler) List(ctx context.Context) ([]types.DomainPackSummary, error) {
	if r == nil || r.packs == nil {
		return nil, apperrors.NewInternalServerError("domain packs are not available")
	}
	now := r.now()
	out := make([]types.DomainPackSummary, 0, len(r.packs.Names()))
	for _, name := range r.packs.Names() {
		pack, _ := r.packs.Get(name)
		summary := packSummary(pack, now)
		agents, err := r.agentsUsing(ctx, name)
		if err != nil {
			return nil, err
		}
		summary.AgentCount = len(agents)
		out = append(out, summary)
	}
	return out, nil
}

// Detail is the read-only view of one pack: its documents inspected
// against the legal catalogue (no request is recorded), its formulas with
// the state of their basis, and the agents using it.
func (r *DomainPackReconciler) Detail(ctx context.Context, name string) (*types.DomainPackDetail, error) {
	if r == nil || r.packs == nil || r.catalog == nil || r.requests == nil || r.reviews == nil {
		return nil, apperrors.NewInternalServerError("domain packs are not available")
	}
	pack, ok := r.packs.Get(strings.TrimSpace(name))
	if !ok {
		return nil, apperrors.NewNotFoundError(fmt.Sprintf("domain pack %q is not in this build", name))
	}
	// stored edits may come from another backend: apply them first
	if err := r.LoadFormulaOverrides(ctx); err != nil {
		return nil, err
	}
	edits, err := r.formulaEdits(ctx, pack.Name())
	if err != nil {
		return nil, err
	}
	now := r.now()
	m := pack.Manifest.Metadata
	out := &types.DomainPackDetail{
		DomainPackSummary: packSummary(pack, now),
		AnswerFooter:      m.AnswerFooter,
		DraftDocuments:    []types.DomainPackDraft{},
		Documents:         []types.DomainPackDocumentView{},
		Formulas:          []types.DomainPackFormulaView{},
		Errors:            []types.DomainPackReconcileError{},
	}
	for _, d := range m.DraftDocuments {
		draft := types.DomainPackDraft{Title: d.Title, Version: d.Version}
		if !d.Date.IsZero() {
			draft.Date = d.Date.Format("2006-01-02")
		}
		out.DraftDocuments = append(out.DraftDocuments, draft)
	}

	// formulas by basis number key, to list them on their documents
	usedBy := map[string][]string{}
	for _, fm := range pack.Formulas() {
		for _, b := range fm.Basis {
			key := vietnamese_legal.CatalogNumberKey(b.Number)
			usedBy[key] = packAppendUnique(usedBy[key], fm.ID)
		}
	}

	warnings := map[string]string{} // number key → why
	if catalogue := pack.Catalogue(); catalogue != nil {
		for _, entry := range catalogue.Documents {
			ref := types.DomainPackCatalogueRef{Number: strings.TrimSpace(entry.Number), Title: entry.Title, UsedFor: entry.UsedFor}
			key := vietnamese_legal.CatalogNumberKey(ref.Number)
			view := types.DomainPackDocumentView{
				DomainPackCatalogueRef: ref,
				Articles:               append([]string{}, entry.Articles...),
				CitedIn:                packNonNil(pack.CitedIn(ref.Number)),
				Formulas:               packNonNil(usedBy[key]),
				Documents:              []types.DomainPackReconcileDocument{},
			}
			lk, err := r.lookup(ctx, now, ref)
			if err != nil {
				logger.Warnf(ctx, "Domain pack %s inspection of %s: %v", pack.Name(), ref.Number, err)
				out.Errors = append(out.Errors, types.DomainPackReconcileError{Number: ref.Number, Error: err.Error()})
				view.Warning = true
				warnings[key] = "không tra được danh mục: " + err.Error()
				out.Documents = append(out.Documents, view)
				continue
			}
			view.Documents = append(view.Documents, lk.docs...)
			switch len(lk.docs) {
			case 0:
				view.Lookup = types.DomainPackLookupMissing
				warnings[key] = "không có trong danh mục văn bản"
			case 1:
				view.Lookup = types.DomainPackLookupFound
				if note := lk.docs[0].StatusNote; note != "" {
					warnings[key] = note
				} else if !lk.docs[0].HeldInKB {
					warnings[key] = "chưa có toàn văn trong kho tri thức công khai"
				}
			default:
				view.Lookup = types.DomainPackLookupAmbiguous
				warnings[key] = "nhiều văn bản cùng số hiệu"
			}
			view.Warning = warnings[key] != ""
			out.Documents = append(out.Documents, view)
		}
	}

	for _, fm := range pack.Formulas() {
		fv := types.DomainPackFormulaView{
			ID: fm.ID, Name: fm.Name, Expression: fm.Expression, Tool: fm.Tool,
			Variables: []types.DomainPackFormulaVar{},
			Basis:     []types.DomainPackFormulaBasis{},
			Examples:  []types.DomainPackFormulaSample{},
		}
		for _, v := range fm.Variables {
			fv.Variables = append(fv.Variables, types.DomainPackFormulaVar{
				Name: v.Name, Description: v.Description, Unit: v.Unit, Source: v.Source,
			})
		}
		for _, b := range fm.Basis {
			basis := types.DomainPackFormulaBasis{Number: b.Number, Article: b.Article, Status: types.DomainPackBasisOK}
			if note := warnings[vietnamese_legal.CatalogNumberKey(b.Number)]; note != "" {
				basis.Status, basis.Note = types.DomainPackBasisWarning, note
				fv.Warning = true
			}
			fv.Basis = append(fv.Basis, basis)
		}
		for _, ex := range fm.Examples {
			fv.Examples = append(fv.Examples, types.DomainPackFormulaSample{Inputs: ex.Inputs, Expected: ex.Expected})
		}
		if row := edits[fm.ID]; row != nil && !pack.FormulaEdited(fm.ID) {
			// stored but refused by this build (see LoadFormulaOverrides)
			fv.EditNotApplied = true
		} else if row != nil {
			at := row.UpdatedAt
			fv.Edited, fv.EditedBy, fv.EditedAt = true, r.editorName(ctx, row.UpdatedBy), &at
			if shipped, ok := pack.ShippedFormula(fm.ID); ok {
				orig := editFromFormula(shipped)
				fv.Shipped = &orig
			}
		}
		out.Formulas = append(out.Formulas, fv)
	}

	agents, err := r.agentsUsing(ctx, pack.Name())
	if err != nil {
		return nil, err
	}
	out.Agents = agents
	out.AgentCount = len(agents)
	return out, nil
}

func (r *DomainPackReconciler) agentsUsing(ctx context.Context, name string) ([]types.DomainPackAgent, error) {
	if r.agents == nil {
		return []types.DomainPackAgent{}, nil
	}
	agents, err := r.agents.ListAgentsUsingPack(ctx, name)
	if err != nil {
		return nil, fmt.Errorf("agents using %s: %w", name, err)
	}
	if agents == nil {
		agents = []types.DomainPackAgent{}
	}
	return agents, nil
}

func packSummary(pack *domainpacks.Pack, now time.Time) types.DomainPackSummary {
	m := pack.Manifest.Metadata
	out := types.DomainPackSummary{
		Name:          pack.Name(),
		Description:   pack.Manifest.Description,
		Version:       m.Version,
		AsOf:          m.AsOf.Format("2006-01-02"),
		ReviewBy:      m.ReviewBy.Format("2006-01-02"),
		Overdue:       pack.Overdue(now),
		Domain:        m.Domain,
		Licence:       m.Licence,
		Attribution:   m.Attribution,
		Origin:        m.Origin,
		RequiredTools: append([]string{}, m.RequiredTools...),
		FormulaCount:  len(pack.Formulas()),
	}
	if c := pack.Catalogue(); c != nil {
		out.DocumentCount = len(c.Documents)
	}
	return out
}

func packAppendUnique(list []string, v string) []string {
	for _, x := range list {
		if x == v {
			return list
		}
	}
	return append(list, v)
}

func packNonNil(list []string) []string {
	if list == nil {
		return []string{}
	}
	sort.Strings(list)
	return list
}
