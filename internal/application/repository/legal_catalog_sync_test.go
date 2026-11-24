package repository

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Tencent/WeKnora/internal/types"
)

func legalRelationRows(t *testing.T, repo *legalCatalogRepository) map[string]types.LegalDocumentRelation {
	t.Helper()
	var rows []types.LegalDocumentRelation
	require.NoError(t, repo.db.Find(&rows).Error)
	out := map[string]types.LegalDocumentRelation{}
	for _, r := range rows {
		out[r.FromID+">"+r.ToID+":"+string(r.Relation)] = r
	}
	return out
}

// The import writes only the difference: unchanged rows keep created_at,
// gone ones are removed, new ones added, changed labels rewritten; text
// and other-source edges are not touched, and a listed key already held as
// a text edge stays the text's.
func TestLegalCatalogSyncRelationsByDifference(t *testing.T) {
	repo := newLegalCatalogRepo(t)
	ctx := context.Background()
	require.NoError(t, repo.UpsertDocuments(ctx, []*types.LegalDocument{
		{ID: "luat", Source: "vbpl"}, {ID: "nd", Source: "vbpl"}, {ID: "tt", Source: "vbpl"},
		{ID: "hd", Source: "dangcongsan"},
	}))
	old := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	rel := func(from, to string, r types.LegalRelation, label string) *types.LegalDocumentRelation {
		return &types.LegalDocumentRelation{FromID: from, ToID: to, Relation: r, SourceLabel: label, CreatedAt: old}
	}
	require.NoError(t, repo.db.Create([]*types.LegalDocumentRelation{
		rel("nd", "luat", types.LegalRelationGuides, "Quy định chi tiết"),               // unchanged
		rel("tt", "nd", types.LegalRelationGuides, "Hướng dẫn"),                         // relabelled
		rel("tt", "luat", types.LegalRelationBasedOn, "Căn cứ"),                         // gone from the list
		rel("luat", "nd", types.LegalRelationSucceeds, types.LegalRelationSourceText),   // text, listed again by vbpl
		rel("tt", "luat", types.LegalRelationReferences, types.LegalRelationSourceText), // text, not listed
		rel("hd", "luat", types.LegalRelationBasedOn, "Căn cứ"),                         // another source's document
	}).Error)

	st, err := repo.SyncRelations(ctx, []*types.LegalDocumentRelation{
		{FromID: "nd", ToID: "luat", Relation: types.LegalRelationGuides, SourceLabel: "Quy định chi tiết"},
		{FromID: "tt", ToID: "nd", Relation: types.LegalRelationGuides, SourceLabel: "Hướng dẫn thi hành"},
		{FromID: "luat", ToID: "nd", Relation: types.LegalRelationSucceeds, SourceLabel: "Thay thế"},
		{FromID: "nd", ToID: "tt", Relation: types.LegalRelationReferences, SourceLabel: "Dẫn chiếu"},
		{FromID: "nd", ToID: "tt", Relation: types.LegalRelationReferences, SourceLabel: "repeat"},
	}, types.LegalRelationSyncOptions{})
	require.NoError(t, err)
	require.Equal(t, types.LegalRelationSyncStats{Added: 1, Removed: 1, Relabelled: 1, Unchanged: 1, HeldElsewhere: 1}, st)

	rows := legalRelationRows(t, repo)
	require.Len(t, rows, 6)
	require.True(t, rows["nd>luat:guides"].CreatedAt.Equal(old), "an unchanged row keeps created_at")
	require.Equal(t, "Hướng dẫn thi hành", rows["tt>nd:guides"].SourceLabel)
	require.True(t, rows["tt>nd:guides"].CreatedAt.Equal(old), "a relabelled row keeps created_at")
	require.NotContains(t, rows, "tt>luat:based_on")
	require.Equal(t, types.LegalRelationSourceText, rows["luat>nd:succeeds"].SourceLabel, "the text's edge stays the text's")
	require.Contains(t, rows, "tt>luat:references", "a text edge the list lacks stays")
	require.Contains(t, rows, "hd>luat:based_on", "an edge of another source stays")
	require.Equal(t, "Dẫn chiếu", rows["nd>tt:references"].SourceLabel, "the first of a repeated key")

	// the same list again changes nothing
	st, err = repo.SyncRelations(ctx, []*types.LegalDocumentRelation{
		{FromID: "nd", ToID: "luat", Relation: types.LegalRelationGuides, SourceLabel: "Quy định chi tiết"},
		{FromID: "tt", ToID: "nd", Relation: types.LegalRelationGuides, SourceLabel: "Hướng dẫn thi hành"},
		{FromID: "luat", ToID: "nd", Relation: types.LegalRelationSucceeds, SourceLabel: "Thay thế"},
		{FromID: "nd", ToID: "tt", Relation: types.LegalRelationReferences, SourceLabel: "Dẫn chiếu"},
	}, types.LegalRelationSyncOptions{})
	require.NoError(t, err)
	require.Equal(t, types.LegalRelationSyncStats{Unchanged: 3, HeldElsewhere: 1}, st)
}

// A list that would remove more than max(1000, 10 %) of the owned edges is
// refused with nothing written, unless the removal is allowed.
func TestLegalCatalogSyncRelationsRefusesMassRemoval(t *testing.T) {
	require.Equal(t, 1000, LegalRelationRemovalLimit(5000))
	require.Equal(t, 13200, LegalRelationRemovalLimit(132000))

	repo := newLegalCatalogRepo(t)
	ctx := context.Background()
	const n = 1200
	docs := []*types.LegalDocument{{ID: "luat", Source: "vbpl"}}
	var rels []*types.LegalDocumentRelation
	for i := 0; i < n; i++ {
		id := fmt.Sprintf("tt%04d", i)
		docs = append(docs, &types.LegalDocument{ID: id, Source: "vbpl"})
		rels = append(rels, &types.LegalDocumentRelation{FromID: id, ToID: "luat", Relation: types.LegalRelationBasedOn, SourceLabel: "Căn cứ"})
	}
	require.NoError(t, repo.UpsertDocuments(ctx, docs))
	st, err := repo.SyncRelations(ctx, rels, types.LegalRelationSyncOptions{})
	require.NoError(t, err)
	require.Equal(t, n, st.Added)

	// a partial file: 1,100 of 1,200 gone
	partial := rels[:100]
	_, err = repo.SyncRelations(ctx, partial, types.LegalRelationSyncOptions{})
	var mass *ErrLegalRelationMassRemoval
	require.True(t, errors.As(err, &mass), "%v", err)
	require.Equal(t, ErrLegalRelationMassRemoval{Remove: 1100, Owned: n, Limit: 1000}, *mass)
	require.Len(t, legalRelationRows(t, repo), n, "nothing written")

	// a removal within the limit passes
	st, err = repo.SyncRelations(ctx, rels[:300], types.LegalRelationSyncOptions{})
	require.NoError(t, err)
	require.Equal(t, 900, st.Removed)

	// the override lets a real mass removal through
	st, err = repo.SyncRelations(ctx, nil, types.LegalRelationSyncOptions{AllowMassRemoval: true})
	require.NoError(t, err)
	require.Equal(t, 300, st.Removed)
	require.Empty(t, legalRelationRows(t, repo))
}
