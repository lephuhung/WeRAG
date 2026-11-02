package service

import (
	"context"
	"fmt"
	"maps"
	"strings"
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

// LoadFormulaOverrides applies the stored edits to the packs. An edit the
// build no longer accepts is logged and skipped (the shipped formula
// applies); the row is kept for the SuperAdmin to fix or reset.
func (r *DomainPackReconciler) LoadFormulaOverrides(ctx context.Context) error {
	_, err := r.loadFormulaOverrides(ctx)
	return err
}

// loadFormulaOverrides is LoadFormulaOverrides returning the stored rows.
func (r *DomainPackReconciler) loadFormulaOverrides(ctx context.Context) ([]*types.DomainPackFormulaOverride, error) {
	if r == nil || r.packs == nil || r.overrides == nil {
		return nil, nil
	}
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
	for name, edits := range byPack {
		if _, ok := r.packs.Get(name); !ok {
			logger.Warnf(ctx, "domain pack %s: %d formula edit(s) for a pack not in this build", name, len(edits))
		}
	}
	for _, name := range r.packs.Names() {
		pack, _ := r.packs.Get(name)
		for id, problems := range pack.SetFormulaOverrides(byPack[name]) {
			logger.Warnf(ctx, "domain pack %s: formula edit %s not applied: %s", name, id, strings.Join(problems, "; "))
		}
	}
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
	if err := r.overrides.Upsert(ctx, row); err != nil {
		return nil, fmt.Errorf("save formula edit: %w", err)
	}
	pack.SetFormulaOverride(checked)
	logger.Infof(ctx, "domain pack %s: formula %s edited by %s", pack.Name(), checked.ID, row.UpdatedBy)
	return r.Detail(ctx, pack.Name())
}

// ResetFormula drops the edit of formula id: formulas.yaml applies again.
func (r *DomainPackReconciler) ResetFormula(ctx context.Context, name, id string) (*types.DomainPackDetail, error) {
	pack, err := r.editablePack(name)
	if err != nil {
		return nil, err
	}
	if _, ok := pack.ShippedFormula(id); !ok {
		return nil, apperrors.NewNotFoundError(fmt.Sprintf("formula %q is not in pack %q", id, pack.Name()))
	}
	if err := r.overrides.Delete(ctx, pack.Name(), id); err != nil {
		return nil, fmt.Errorf("delete formula edit: %w", err)
	}
	pack.ClearFormulaOverride(id)
	logger.Infof(ctx, "domain pack %s: formula %s reset by %s", pack.Name(), id, types.CallerFromContext(ctx).UserID)
	return r.Detail(ctx, pack.Name())
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

// editorName is the user name (or e-mail) of user id, the id when unknown.
func (r *DomainPackReconciler) editorName(ctx context.Context, id string) string {
	if r.users == nil || id == "" {
		return id
	}
	u, err := r.users.GetUserByID(ctx, id)
	if err != nil || u == nil {
		return id
	}
	if u.Username != "" {
		return u.Username
	}
	return u.Email
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
