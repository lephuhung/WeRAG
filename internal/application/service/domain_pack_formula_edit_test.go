package service

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	domainpackassets "github.com/Tencent/WeKnora/domain-packs"
	"github.com/Tencent/WeKnora/internal/agent/domainpacks"
	apperrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/types"
)

const editPack = "opc-vn-business-law"

// shippedEdit is formula id of the test pack as an edit body.
func shippedEdit(t *testing.T, r *DomainPackReconciler, id string) types.DomainPackFormulaEdit {
	t.Helper()
	pack, ok := r.packs.Get(editPack)
	require.True(t, ok)
	fm, ok := pack.ShippedFormula(id)
	require.True(t, ok)
	return editFromFormula(fm)
}

func formulaView(t *testing.T, d *types.DomainPackDetail, id string) types.DomainPackFormulaView {
	t.Helper()
	for _, f := range d.Formulas {
		if f.ID == id {
			return f
		}
	}
	t.Fatalf("formula %s not in detail", id)
	return types.DomainPackFormulaView{}
}

// An edit is stored, served to the model (FormulaBlock) and shown as
// edited; reset brings formulas.yaml back.
func TestDomainPackFormulaEditAndReset(t *testing.T) {
	r, _ := newReconcileFixture(t)
	ctx := reconcileCallerCtx()
	edit := shippedEdit(t, r, "bien_hoa_von")
	edit.Name = "Biên hòa vốn (đã sửa)"
	edit.Expression = "Biên hòa vốn = Tỷ lệ % doanh thu ÷ 100 ÷ Thuế suất TNDN"
	edit.Variables[0].Description = "Tỷ lệ TNDN trên doanh thu theo ngành"
	edit.Examples = append(edit.Examples, types.DomainPackFormulaSample{
		Inputs:   map[string]float64{"ty_le_doanh_thu": 2, "thue_suat": 0.2},
		Expected: map[string]float64{"bien_hoa_von": 0.1},
	})

	d, err := r.UpdateFormula(ctx, editPack, "bien_hoa_von", edit)
	require.NoError(t, err)
	v := formulaView(t, d, "bien_hoa_von")
	require.True(t, v.Edited)
	require.Equal(t, "super-1", v.EditedBy)
	require.NotNil(t, v.EditedAt)
	require.Equal(t, edit.Name, v.Name)
	require.Equal(t, "compare_tax_methods", v.Tool)
	require.Len(t, v.Examples, 2)
	require.NotNil(t, v.Shipped)
	require.NotEqual(t, edit.Name, v.Shipped.Name)
	require.Contains(t, r.packs.FormulaBlock("compare_tax_methods"), edit.Expression)

	// a fresh registry picks the stored edit up at load (another backend)
	r2 := *r
	fresh, err := domainpacks.Load(domainpackassets.FS)
	require.NoError(t, err)
	r2.packs = fresh
	require.NoError(t, r2.LoadFormulaOverrides(context.Background()))
	require.Contains(t, fresh.FormulaBlock("compare_tax_methods"), edit.Expression)

	d, err = r.ResetFormula(ctx, editPack, "bien_hoa_von")
	require.NoError(t, err)
	v = formulaView(t, d, "bien_hoa_von")
	require.False(t, v.Edited)
	require.Nil(t, v.Shipped)
	require.NotContains(t, r.packs.FormulaBlock("compare_tax_methods"), edit.Expression)
}

// Edits the Go implementation cannot honour are refused with the reasons.
func TestDomainPackFormulaEditRefused(t *testing.T) {
	r, _ := newReconcileFixture(t)
	ctx := reconcileCallerCtx()
	cases := map[string]func(e *types.DomainPackFormulaEdit){
		"variable renamed": func(e *types.DomainPackFormulaEdit) { e.Variables[0].Name = "ty_le" },
		"wrong example":    func(e *types.DomainPackFormulaEdit) { e.Examples[0].Expected["bien_hoa_von"] = 0.5 },
		"constant rate":    func(e *types.DomainPackFormulaEdit) { e.Expression = "Biên = Tỷ lệ ÷ 20 %" },
		"basis not in catalogue": func(e *types.DomainPackFormulaEdit) {
			e.Basis = append(e.Basis, types.DomainPackFormulaBasisRef{Number: "999/2099/NĐ-CP"})
		},
		"empty name":          func(e *types.DomainPackFormulaEdit) { e.Name = "  " },
		"bad source":          func(e *types.DomainPackFormulaEdit) { e.Variables[1].Source = "const" },
		"no examples":         func(e *types.DomainPackFormulaEdit) { e.Examples = nil },
		"missing input":       func(e *types.DomainPackFormulaEdit) { delete(e.Examples[0].Inputs, "thue_suat") },
		"two-line expression": func(e *types.DomainPackFormulaEdit) { e.Expression += "\nthêm dòng" },
		"duplicate basis":     func(e *types.DomainPackFormulaEdit) { e.Basis = append(e.Basis, e.Basis[0]) },
		"unknown output": func(e *types.DomainPackFormulaEdit) {
			e.Examples[0].Expected["khong_co"] = 1
		},
	}
	shipped := shippedEdit(t, r, "bien_hoa_von")
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			edit := shippedEdit(t, r, "bien_hoa_von")
			mutate(&edit)
			_, err := r.UpdateFormula(ctx, editPack, "bien_hoa_von", edit)
			require.Error(t, err)
			appErr, ok := apperrors.IsAppError(err)
			require.True(t, ok, "%v", err)
			require.Equal(t, apperrors.ErrValidation, appErr.Code)
			pack, _ := r.packs.Get(editPack)
			require.False(t, pack.FormulaEdited("bien_hoa_von"))
		})
	}
	// the cases changed copies only: the shipped formula is still accepted
	require.Equal(t, shipped, shippedEdit(t, r, "bien_hoa_von"))
	_, err := r.UpdateFormula(ctx, editPack, "bien_hoa_von", shipped)
	require.NoError(t, err)
	_, err = r.UpdateFormula(ctx, editPack, "khong_co", shippedEdit(t, r, "bien_hoa_von"))
	require.Error(t, err)
	_, err = r.UpdateFormula(ctx, "khong-co-goi", "bien_hoa_von", shippedEdit(t, r, "bien_hoa_von"))
	require.Error(t, err)
	_, err = r.ResetFormula(ctx, editPack, "khong_co")
	require.Error(t, err)
}

// A stored edit this build refuses is skipped and flagged on the page.
func TestDomainPackFormulaEditNotAppliedWhenStale(t *testing.T) {
	r, db := newReconcileFixture(t)
	edit := shippedEdit(t, r, "bien_hoa_von")
	edit.Variables[0].Name = "doi_ten"
	require.NoError(t, db.Create(&types.DomainPackFormulaOverride{
		PackName: editPack, FormulaID: "bien_hoa_von", Formula: edit, UpdatedBy: "super-1",
	}).Error)
	d, err := r.Detail(reconcileCallerCtx(), editPack)
	require.NoError(t, err)
	v := formulaView(t, d, "bien_hoa_von")
	require.True(t, v.EditNotApplied)
	require.False(t, v.Edited)
	require.False(t, strings.Contains(v.Name, "doi_ten"))
}
