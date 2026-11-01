package service

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	domainpackassets "github.com/Tencent/WeKnora/domain-packs"
	"github.com/Tencent/WeKnora/internal/agent/domainpacks"
	apperrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
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
		"amount in the expression": func(e *types.DomainPackFormulaEdit) {
			e.Expression = "Biên hòa vốn = (Tỷ lệ % doanh thu ÷ 100) ÷ 0,2"
		},
		"rate in the name":    func(e *types.DomainPackFormulaEdit) { e.Name = "Biên hòa vốn với thuế suất 0.2" },
		"percent in the name": func(e *types.DomainPackFormulaEdit) { e.Name = "Biên hòa vốn theo 100 %" },
		"amount behind an allowed number": func(e *types.DomainPackFormulaEdit) {
			e.Expression = "Biên hòa vốn = Tỷ lệ % doanh thu ÷ 100 ÷ Thuế suất (0 nếu doanh thu năm 500 triệu trở xuống)"
		},
		"article not in the catalogue": func(e *types.DomainPackFormulaEdit) {
			e.Basis = []types.DomainPackFormulaBasisRef{{Number: "67/2025/QH15", Article: "Điều 10"}}
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

// An edit of a formula (or a pack) this build no longer ships is listed in
// the detail's errors, can be deleted, and is logged once, not every
// refresh.
func TestDomainPackFormulaEditOrphans(t *testing.T) {
	r, db := newReconcileFixture(t)
	ctx := reconcileCallerCtx()
	edit := shippedEdit(t, r, "bien_hoa_von")
	require.NoError(t, db.Create(&types.DomainPackFormulaOverride{
		PackName: editPack, FormulaID: "da_bo", Formula: edit, UpdatedBy: "super-1",
	}).Error)
	require.NoError(t, db.Create(&types.DomainPackFormulaOverride{
		PackName: "goi-da-bo", FormulaID: "bien_hoa_von", Formula: edit, UpdatedBy: "super-1",
	}).Error)

	d, err := r.Detail(ctx, editPack)
	require.NoError(t, err)
	var orphans []string
	for _, e := range d.Errors {
		if e.FormulaID != "" {
			orphans = append(orphans, e.FormulaID)
		}
	}
	require.Equal(t, []string{"da_bo"}, orphans)

	// logged once per stale row, forgotten when it goes away
	logged := map[string]string{}
	for k, v := range r.editState().logged {
		logged[k] = v
	}
	require.Contains(t, logged, editPack+"|da_bo")
	require.Contains(t, logged, "goi-da-bo|*")
	require.NoError(t, r.LoadFormulaOverrides(context.Background()))
	require.Equal(t, logged, r.editState().logged)

	d, err = r.ResetFormula(ctx, editPack, "da_bo")
	require.NoError(t, err)
	for _, e := range d.Errors {
		require.Empty(t, e.FormulaID)
	}
	d, err = r.ResetFormula(ctx, "goi-da-bo", "bien_hoa_von")
	require.NoError(t, err)
	require.Equal(t, "goi-da-bo", d.Name)
	var left int64
	require.NoError(t, db.Model(&types.DomainPackFormulaOverride{}).Count(&left).Error)
	require.Zero(t, left)
	require.NoError(t, r.LoadFormulaOverrides(context.Background()))
	require.Empty(t, r.editState().logged)

	_, err = r.ResetFormula(ctx, "goi-da-bo", "bien_hoa_von")
	require.Error(t, err, "nothing stored any more")
}

// failingListRepo fails List; it stands for an unreadable table.
type failingListRepo struct {
	interfaces.DomainPackFormulaOverrideRepository
}

func (failingListRepo) List(context.Context) ([]*types.DomainPackFormulaOverride, error) {
	return nil, errors.New("bảng không đọc được")
}

// The pack page still shows when the edits cannot be read, with the edits
// held in memory.
func TestDomainPackDetailSurvivesUnreadableEdits(t *testing.T) {
	r, _ := newReconcileFixture(t)
	ctx := reconcileCallerCtx()
	edit := shippedEdit(t, r, "bien_hoa_von")
	edit.Name = "Biên hòa vốn (đã sửa)"
	_, err := r.UpdateFormula(ctx, editPack, "bien_hoa_von", edit)
	require.NoError(t, err)

	r.overrides = failingListRepo{r.overrides}
	d, err := r.Detail(ctx, editPack)
	require.NoError(t, err)
	require.NotEmpty(t, d.Errors)
	v := formulaView(t, d, "bien_hoa_von")
	require.True(t, v.Edited)
	require.Equal(t, edit.Name, v.Name)
}

// slowListRepo holds the first List after reading (a reload racing a
// save) and the second one (the save's own page reload) until released.
type slowListRepo struct {
	interfaces.DomainPackFormulaOverrideRepository
	mu                     sync.Mutex
	calls                  int
	read, release          chan struct{}
	secondRead, secondHold chan struct{}
}

func (s *slowListRepo) List(ctx context.Context) ([]*types.DomainPackFormulaOverride, error) {
	rows, err := s.DomainPackFormulaOverrideRepository.List(ctx)
	s.mu.Lock()
	s.calls++
	n := s.calls
	s.mu.Unlock()
	switch n {
	case 1:
		close(s.read)
		<-s.release
	case 2:
		close(s.secondRead)
		<-s.secondHold
	}
	return rows, err
}

// A reload that read the table before a save cannot wipe the saved edit.
func TestDomainPackFormulaReloadDoesNotWipeSave(t *testing.T) {
	r, _ := newReconcileFixture(t)
	ctx := reconcileCallerCtx()
	slow := &slowListRepo{DomainPackFormulaOverrideRepository: r.overrides,
		read: make(chan struct{}), release: make(chan struct{}),
		secondRead: make(chan struct{}), secondHold: make(chan struct{})}
	r.overrides = slow
	edit := shippedEdit(t, r, "bien_hoa_von")
	edit.Name = "Biên hòa vốn (đã sửa)"
	pack, _ := r.packs.Get(editPack)

	reloaded := make(chan error)
	go func() { reloaded <- r.LoadFormulaOverrides(context.Background()) }()
	<-slow.read // the reload holds the table as it was: no edit
	saved := make(chan error)
	go func() {
		_, err := r.UpdateFormula(ctx, editPack, "bien_hoa_von", edit)
		saved <- err
	}()
	time.Sleep(50 * time.Millisecond) // let the save run if it is not held
	close(slow.release)
	require.NoError(t, <-reloaded)
	<-slow.secondRead // the save is applied; its page reload has not run
	require.True(t, pack.FormulaEdited("bien_hoa_von"), "the older reload wiped the saved edit")
	close(slow.secondHold)
	require.NoError(t, <-saved)
	require.Contains(t, r.packs.FormulaBlock("compare_tax_methods"), edit.Name)
}

// countingUsers answers GetUsersByIDs and counts the calls.
type countingUsers struct {
	interfaces.UserRepository
	calls int
}

func (c *countingUsers) GetUsersByIDs(_ context.Context, ids []string) (map[string]*types.User, error) {
	c.calls++
	out := map[string]*types.User{}
	for _, id := range ids {
		if id == "super-1" {
			out[id] = &types.User{ID: id, Username: "quan_tri"}
		}
	}
	return out, nil
}

// The editor names of a pack page are read in one query.
func TestDomainPackFormulaEditorNamesBatched(t *testing.T) {
	r, db := newReconcileFixture(t)
	users := &countingUsers{}
	r.users = users
	for _, id := range []string{"bien_hoa_von", "thue_theo_loi_nhuan", "thue_theo_ty_le_doanh_thu"} {
		by := "super-1"
		if id == "thue_theo_ty_le_doanh_thu" {
			by = "khong-ro"
		}
		require.NoError(t, db.Create(&types.DomainPackFormulaOverride{
			PackName: editPack, FormulaID: id, Formula: shippedEdit(t, r, id), UpdatedBy: by,
		}).Error)
	}
	d, err := r.Detail(reconcileCallerCtx(), editPack)
	require.NoError(t, err)
	require.Equal(t, 1, users.calls)
	require.Equal(t, "quan_tri", formulaView(t, d, "bien_hoa_von").EditedBy)
	require.Equal(t, "quan_tri", formulaView(t, d, "thue_theo_loi_nhuan").EditedBy)
	require.Equal(t, "khong-ro", formulaView(t, d, "thue_theo_ty_le_doanh_thu").EditedBy)
}

// An edit made over an older formulas.yaml wording is flagged; saving it
// again over the current wording clears the flag.
func TestDomainPackFormulaShippedChanged(t *testing.T) {
	r, db := newReconcileFixture(t)
	ctx := reconcileCallerCtx()
	edit := shippedEdit(t, r, "bien_hoa_von")
	edit.Name = "Biên hòa vốn (đã sửa)"
	d, err := r.UpdateFormula(ctx, editPack, "bien_hoa_von", edit)
	require.NoError(t, err)
	require.False(t, formulaView(t, d, "bien_hoa_von").ShippedChanged)
	var row types.DomainPackFormulaOverride
	require.NoError(t, db.First(&row, "formula_id = ?", "bien_hoa_von").Error)
	pack, _ := r.packs.Get(editPack)
	require.Equal(t, pack.ShippedFormulaHash("bien_hoa_von"), row.Formula.ShippedHash)

	row.Formula.ShippedHash = "0000000000000000" // made over an older wording
	require.NoError(t, db.Save(&row).Error)
	d, err = r.Detail(ctx, editPack)
	require.NoError(t, err)
	require.True(t, formulaView(t, d, "bien_hoa_von").ShippedChanged)

	row.Formula.ShippedHash = "" // stored before the hash was kept: unknown
	require.NoError(t, db.Save(&row).Error)
	d, err = r.Detail(ctx, editPack)
	require.NoError(t, err)
	require.False(t, formulaView(t, d, "bien_hoa_von").ShippedChanged)

	edit.ShippedHash = "ignored"
	d, err = r.UpdateFormula(ctx, editPack, "bien_hoa_von", edit)
	require.NoError(t, err)
	require.False(t, formulaView(t, d, "bien_hoa_von").ShippedChanged)
}
