package service

import (
	"context"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/agent/tools"
	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/stretchr/testify/require"
)

// The legal basis check on the real catalogue repository (SQLite) with the
// title resolver: laws named with a year resolve to the law of that year,
// in force or not; a name and the number of the same law are one finding.
func TestLegalBasisCheckOnCatalogue(t *testing.T) {
	db := legalNameTestDB(t)
	now := time.Date(2026, 11, 11, 10, 0, 0, 0, time.Local)
	paras := []string{
		"Căn cứ Luật Đất đai năm 2013;",
		"Căn cứ Luật Đất đai 2024 và Luật Đất đai số 31/2024/QH15;",
		"Căn cứ Luật Đấu thầu 2013; Luật Giáo dục; Luật Hư cấu 2020;",
	}
	r, err := tools.CheckLegalBasis(context.Background(), paras, repository.NewLegalCatalogRepository(db),
		legalBasisNames(db, now), now)
	require.NoError(t, err)
	got := map[string]tools.LegalBasisFinding{}
	for _, f := range r.Findings {
		got[f.Citation] = f
	}
	require.Len(t, got, 4, "%+v", r.Findings)
	require.Equal(t, tools.LegalBasisExpired, got["Luật Đất đai 2013"].Status)
	require.Equal(t, "45/2013/QH13", got["Luật Đất đai 2013"].Document.DocumentNumber)
	require.Equal(t, tools.LegalBasisInForce, got["Luật Đất đai 2024"].Status)
	require.Len(t, got["Luật Đất đai 2024"].Positions, 2, "name and number of 31/2024/QH15 are one finding")
	require.Equal(t, tools.LegalBasisExpired, got["Luật Đấu thầu 2013"].Status)
	require.Equal(t, "43/2013/QH13", got["Luật Đấu thầu 2013"].Document.DocumentNumber)
	require.Len(t, got["Luật Đấu thầu 2013"].Related, 1)
	require.Equal(t, tools.LegalBasisAmendedBy, got["Luật Đấu thầu 2013"].Related[0].Relation)
	require.Equal(t, tools.LegalBasisNotFound, got["Luật Hư cấu 2020"].Status)
	require.Equal(t, 1, r.Skipped.NamesWithoutYear, "Luật Giáo dục: the law in force, nothing to check")

	// a name read only through a shorter title is a guess, never the answer
	res, err := legalBasisNames(db, now)(context.Background(), "Luật Đất đai cho phép", "2024")
	require.NoError(t, err)
	require.Nil(t, res.Chosen)
	require.True(t, res.Guess)
	require.Len(t, res.Candidates, 1)

	// the type filter of a bare number runs in SQL
	cat := repository.NewLegalCatalogRepository(db)
	laws, err := cat.FindByNumberPrefixTypes(context.Background(), "43/2013", []string{"Luật", "Bộ luật"}, 10)
	require.NoError(t, err)
	require.Len(t, laws, 1)
	none, err := cat.FindByNumberPrefixTypes(context.Background(), "43/2013", []string{"Nghị định"}, 10)
	require.NoError(t, err)
	require.Empty(t, none)
}

// legalBasisNoCatalog answers every lookup with nothing.
type legalBasisNoCatalog struct{ interfaces.LegalCatalogRepository }

func (legalBasisNoCatalog) FindByNumberKey(context.Context, string, int) ([]*types.LegalDocument, error) {
	return nil, nil
}

func (legalBasisNoCatalog) FindByNumberPrefixTypes(context.Context, string, []string, int) ([]*types.LegalDocument, error) {
	return nil, nil
}

func (legalBasisNoCatalog) ListLinks(context.Context, string, []types.LegalRelation, []types.LegalRelation) ([]types.LegalRelationLink, error) {
	return nil, nil
}

// Wired as in the container — the workspace service behind the sub-chat
// decorator — a force-save that fails sets unsaved, also from a sub-chat.
func TestLegalBasisCheckUnsavedThroughSubChatWorkspaces(t *testing.T) {
	fx := newDWFixture(t)
	ws := fx.create(t)
	wrapped := NewSubChatWorkspaces(fx.svc, &subChatRootsRepo{roots: map[string]string{"sub-1": "sess-1"}})
	precheck := NewDocumentFormatPrecheck(wrapped, nil, nil, nil)
	precheck.SetLegalBasis(legalBasisNoCatalog{}, nil)

	fx.ds.onCommand = func(map[string]interface{}) int { return onlyOfficeCommandNoChanges }
	r, err := precheck.LegalBasisCheck(context.Background(), 7, "sub-1", ws.ID)
	require.NoError(t, err)
	require.False(t, r.Unsaved)
	require.Equal(t, ws.ID, r.DocumentID)

	fx.ds.onCommand = func(map[string]interface{}) int { return 6 } // the Document Server refuses
	r, err = precheck.LegalBasisCheck(context.Background(), 7, "sub-1", ws.ID)
	require.NoError(t, err)
	require.True(t, r.Unsaved, "the failed save must reach the report through the decorator")
}
