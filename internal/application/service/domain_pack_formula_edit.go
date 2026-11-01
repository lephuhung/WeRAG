package service

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/Tencent/WeKnora/internal/agent/domainpacks"
	apperrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
)

// A SuperAdmin edits the wording of a pack formula; the calculation stays
// the Go implementation (domainpacks/formula_edit.go). Edits live in
// domain_pack_formula_overrides and are applied over the shipped
// formulas.yaml: at startup, on save, and again whenever the pack page is
// read, so a second backend converges on the stored edits.

// formulaEditState is the reconciler's state for the formula edits.
type formulaEditState struct {
	// mu serialises a reload (List → SetFormulaOverrides, a full replace)
	// with a save or reset (write → apply one), so a reload that read the
	// table before a save cannot wipe the saved edit from memory.
	mu sync.Mutex
	// logged is the warning last logged per stored edit (pack|id, or
	// pack|* for a pack not in the build): the 60 s refresh logs a stale
	// row once, and again only when its reasons change or it comes back.
	logMu  sync.Mutex
	logged map[string]string
}

func newFormulaEditState() *formulaEditState {
	return &formulaEditState{logged: map[string]string{}}
}

// fallbackFormulaEditState serves a reconciler built without its constructor.
var fallbackFormulaEditState = newFormulaEditState()

func (r *DomainPackReconciler) editState() *formulaEditState {
	if r.formulaEdits != nil {
		return r.formulaEdits
	}
	return fallbackFormulaEditState
}

// warnOnce logs the warnings (key → message) not logged before with the
// same message and forgets the keys no longer warned about.
func (s *formulaEditState) warnOnce(ctx context.Context, warnings map[string]string) {
	s.logMu.Lock()
	defer s.logMu.Unlock()
	for _, key := range slices.Sorted(maps.Keys(warnings)) {
		if msg := warnings[key]; s.logged[key] != msg {
			logger.Warnf(ctx, "%s", msg)
			s.logged[key] = msg
		}
	}
	for key := range s.logged {
		if _, ok := warnings[key]; !ok {
			delete(s.logged, key)
		}
	}
}

// LoadFormulaOverrides applies the stored edits to the packs. An edit the
// build no longer accepts is logged (once) and skipped (the shipped
// formula applies); the row is kept for the SuperAdmin to fix or reset.
func (r *DomainPackReconciler) LoadFormulaOverrides(ctx context.Context) error {
	_, err := r.loadFormulaOverrides(ctx)
	return err
}

// loadFormulaOverrides is LoadFormulaOverrides returning the stored rows.
func (r *DomainPackReconciler) loadFormulaOverrides(ctx context.Context) ([]*types.DomainPackFormulaOverride, error) {
	if r == nil || r.packs == nil || r.overrides == nil {
		return nil, nil
	}
	st := r.editState()
	st.mu.Lock()
	defer st.mu.Unlock()
	rows, err := r.overrides.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("domain pack formula edits: %w", err)
	}
	byPack := map[string]map[string]domainpacks.Formula{}
	for _, row := range rows {
		if byPack[row.PackName] == nil {
			byPack[row.PackName] = map[string]domainpacks.Formula{}
		}
		byPack[row.PackName][row.FormulaID] = formulaFromEdit(row.FormulaID, row.Formula)
	}
	warnings := map[string]string{}
	for name, edits := range byPack {
		if _, ok := r.packs.Get(name); !ok {
			warnings[name+"|*"] = fmt.Sprintf("domain pack %s: %d formula edit(s) for a pack not in this build (DELETE …/formulas/:id removes them)",
				name, len(edits))
		}
	}
	for _, name := range r.packs.Names() {
		pack, _ := r.packs.Get(name)
		for id, problems := range pack.SetFormulaOverrides(byPack[name]) {
			warnings[name+"|"+id] = fmt.Sprintf("domain pack %s: formula edit %s not applied: %s", name, id, strings.Join(problems, "; "))
		}
	}
	st.warnOnce(ctx, warnings)
	return rows, nil
}

// StartFormulaOverrideRefresh reapplies the stored edits every interval
// until ctx ends, so a save or reset on another backend reaches this one's
// chats without a restart.
func (r *DomainPackReconciler) StartFormulaOverrideRefresh(ctx context.Context, interval time.Duration) {
	if r == nil || r.overrides == nil || interval <= 0 {
		return
	}
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := r.LoadFormulaOverrides(ctx); err != nil {
					logger.Warnf(ctx, "%v", err)
				}
			}
		}
	}()
}

// UpdateFormula stores and applies the edit of formula id of pack name and
// returns the pack detail. An edit that fails the checks is refused with
// the list of problems.
func (r *DomainPackReconciler) UpdateFormula(
	ctx context.Context, name, id string, edit types.DomainPackFormulaEdit,
) (*types.DomainPackDetail, error) {
	pack, err := r.editablePack(name)
	if err != nil {
		return nil, err
	}
	checked, problems := pack.CheckFormulaEdit(formulaFromEdit(id, edit))
	if len(problems) > 0 {
		return nil, apperrors.NewValidationError("the formula edit is not valid: " + strings.Join(problems, "; ")).
			WithDetails(problems)
	}
	row := &types.DomainPackFormulaOverride{
		PackName: pack.Name(), FormulaID: checked.ID, Formula: editFromFormula(checked),
		UpdatedBy: types.CallerFromContext(ctx).UserID,
	}
	row.Formula.ShippedHash = pack.ShippedFormulaHash(checked.ID)
	st := r.editState()
	st.mu.Lock()
	err = r.overrides.Upsert(ctx, row)
	if err == nil {
		pack.SetFormulaOverride(checked)
	}
	st.mu.Unlock()
	if err != nil {
		return nil, fmt.Errorf("save formula edit: %w", err)
	}
	logger.Infof(ctx, "domain pack %s: formula %s edited by %s", pack.Name(), checked.ID, row.UpdatedBy)
	return r.Detail(ctx, pack.Name())
}

// ResetFormula drops the edit of formula id: formulas.yaml applies again.
// It also removes an orphan edit, one whose formula or pack this build no
// longer ships (shown in the pack detail's errors).
func (r *DomainPackReconciler) ResetFormula(ctx context.Context, name, id string) (*types.DomainPackDetail, error) {
	if r == nil || r.packs == nil || r.overrides == nil {
		return nil, apperrors.NewInternalServerError("domain pack formula edits are not available")
	}
	name, id = strings.TrimSpace(name), strings.TrimSpace(id)
	pack, inBuild := r.packs.Get(name)
	if inBuild {
		name = pack.Name()
	}
	shipped := false
	if inBuild {
		_, shipped = pack.ShippedFormula(id)
	}
	if !shipped {
		stored, err := r.formulaEditStored(ctx, name, id)
		if err != nil {
			return nil, err
		}
		if !stored {
			return nil, apperrors.NewNotFoundError(fmt.Sprintf("formula %q is not in pack %q and has no stored edit", id, name))
		}
	}
	st := r.editState()
	st.mu.Lock()
	err := r.overrides.Delete(ctx, name, id)
	if err == nil && inBuild {
		pack.ClearFormulaOverride(id)
	}
	st.mu.Unlock()
	if err != nil {
		return nil, fmt.Errorf("delete formula edit: %w", err)
	}
	logger.Infof(ctx, "domain pack %s: formula %s reset by %s", name, id, types.CallerFromContext(ctx).UserID)
	if !inBuild {
		// no page for a pack this build does not ship
		return &types.DomainPackDetail{
			DomainPackSummary: types.DomainPackSummary{Name: name, RequiredTools: []string{}},
			DraftDocuments:    []types.DomainPackDraft{}, Documents: []types.DomainPackDocumentView{},
			Formulas: []types.DomainPackFormulaView{}, Agents: []types.DomainPackAgent{},
			Errors: []types.DomainPackReconcileError{},
		}, nil
	}
	return r.Detail(ctx, name)
}

// formulaEditStored reports whether an edit of formula id of pack name is
// stored.
func (r *DomainPackReconciler) formulaEditStored(ctx context.Context, name, id string) (bool, error) {
	rows, err := r.overrides.List(ctx)
	if err != nil {
		return false, fmt.Errorf("domain pack formula edits: %w", err)
	}
	for _, row := range rows {
		if row.PackName == name && row.FormulaID == id {
			return true, nil
		}
	}
	return false, nil
}

func (r *DomainPackReconciler) editablePack(name string) (*domainpacks.Pack, error) {
	if r == nil || r.packs == nil || r.overrides == nil {
		return nil, apperrors.NewInternalServerError("domain pack formula edits are not available")
	}
	pack, ok := r.packs.Get(strings.TrimSpace(name))
	if !ok {
		return nil, apperrors.NewNotFoundError(fmt.Sprintf("domain pack %q is not in this build", name))
	}
	return pack, nil
}

// formulaEdits are the stored edits of pack name by formula id, for the
// detail view (who and when).
func formulaEdits(rows []*types.DomainPackFormulaOverride, name string) map[string]*types.DomainPackFormulaOverride {
	out := map[string]*types.DomainPackFormulaOverride{}
	for _, row := range rows {
		if row.PackName == name {
			out[row.FormulaID] = row
		}
	}
	return out
}

// editorNames maps the user ids of rows to their user name (or e-mail),
// in one query; an unknown id maps to itself.
func (r *DomainPackReconciler) editorNames(ctx context.Context, rows map[string]*types.DomainPackFormulaOverride) map[string]string {
	out := map[string]string{}
	var ids []string
	for _, row := range rows {
		if id := row.UpdatedBy; id != "" {
			if _, ok := out[id]; !ok {
				out[id] = id
				ids = append(ids, id)
			}
		}
	}
	if r.users == nil || len(ids) == 0 {
		return out
	}
	users, err := r.users.GetUsersByIDs(ctx, ids)
	if err != nil {
		logger.Warnf(ctx, "domain pack formula editors: %v", err)
		return out
	}
	for id, u := range users {
		switch {
		case u == nil:
		case u.Username != "":
			out[id] = u.Username
		case u.Email != "":
			out[id] = u.Email
		}
	}
	return out
}

func formulaFromEdit(id string, e types.DomainPackFormulaEdit) domainpacks.Formula {
	fm := domainpacks.Formula{ID: id, Name: e.Name, Expression: e.Expression}
	for _, v := range e.Variables {
		fm.Variables = append(fm.Variables, domainpacks.FormulaVariable{
			Name: v.Name, Description: v.Description, Unit: v.Unit, Source: v.Source,
		})
	}
	for _, b := range e.Basis {
		fm.Basis = append(fm.Basis, domainpacks.FormulaBasis{Number: b.Number, Article: b.Article})
	}
	for _, ex := range e.Examples {
		fm.Examples = append(fm.Examples, domainpacks.FormulaExample{Inputs: maps.Clone(ex.Inputs), Expected: maps.Clone(ex.Expected)})
	}
	return fm
}

func editFromFormula(fm domainpacks.Formula) types.DomainPackFormulaEdit {
	e := types.DomainPackFormulaEdit{
		Name: fm.Name, Expression: fm.Expression,
		Variables: []types.DomainPackFormulaVar{},
		Basis:     []types.DomainPackFormulaBasisRef{},
		Examples:  []types.DomainPackFormulaSample{},
	}
	for _, v := range fm.Variables {
		e.Variables = append(e.Variables, types.DomainPackFormulaVar{
			Name: v.Name, Description: v.Description, Unit: v.Unit, Source: v.Source,
		})
	}
	for _, b := range fm.Basis {
		e.Basis = append(e.Basis, types.DomainPackFormulaBasisRef{Number: b.Number, Article: b.Article})
	}
	for _, ex := range fm.Examples {
		e.Examples = append(e.Examples, types.DomainPackFormulaSample{Inputs: maps.Clone(ex.Inputs), Expected: maps.Clone(ex.Expected)})
	}
	return e
}
