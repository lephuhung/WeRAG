package service

import (
	"context"
	"fmt"
	"testing"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func newLegalExploreFixture(t *testing.T) (*gorm.DB, *legalGraphExploreService) {
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&types.LegalDocument{}, &types.LegalDocumentRelation{}))
	svc := NewLegalGraphExploreService(repository.NewLegalCatalogRepository(db), nil, nil).(*legalGraphExploreService)
	return db, svc
}

func exploreDoc(t *testing.T, db *gorm.DB, id, typ string) {
	require.NoError(t, db.Create(&types.LegalDocument{ID: id, Source: "vbpl", DocumentNumber: id, NumberKey: id,
		DocType: typ, Title: typ + " " + id, Status: "Còn hiệu lực"}).Error)
}

func exploreEdge(t *testing.T, db *gorm.DB, from, to string, rel types.LegalRelation) {
	require.NoError(t, db.Create(&types.LegalDocumentRelation{FromID: from, ToID: to, Relation: rel}).Error)
}

// A law, the decrees guiding it, the circulars guiding the decrees; what
// the law replaced and what that one replaced before it. Each side walks
// its own way: the decree's own căn cứ (another law) is not reached.
func TestLegalGraphExplore(t *testing.T) {
	db, svc := newLegalExploreFixture(t)
	ctx := context.Background()
	exploreDoc(t, db, "law", "Luật")
	exploreDoc(t, db, "nd1", "Nghị định")
	exploreDoc(t, db, "nd2", "Nghị định")
	exploreDoc(t, db, "tt1", "Thông tư")
	exploreDoc(t, db, "tt2", "Thông tư")
	exploreDoc(t, db, "old", "Luật")
	exploreDoc(t, db, "older", "Luật")
	exploreDoc(t, db, "otherlaw", "Luật")
	exploreEdge(t, db, "nd1", "law", types.LegalRelationGuides)
	exploreEdge(t, db, "nd1", "law", types.LegalRelationBasedOn)
	exploreEdge(t, db, "nd2", "law", types.LegalRelationBasedOn)
	exploreEdge(t, db, "tt1", "nd1", types.LegalRelationGuides)
	exploreEdge(t, db, "tt2", "nd2", types.LegalRelationGuides)
	exploreEdge(t, db, "tt2", "tt1", types.LegalRelationAmends) // a relation inside one step: kept as an edge
	exploreEdge(t, db, "law", "old", types.LegalRelationReplaces)
	exploreEdge(t, db, "old", "older", types.LegalRelationReplaces)
	exploreEdge(t, db, "nd1", "otherlaw", types.LegalRelationBasedOn) // the decree's own căn cứ: other side, not walked
	exploreEdge(t, db, "law", "law", types.LegalRelationReferences)   // not a drawn relation, and a self edge

	res, err := svc.Explore(ctx, "law", 2)
	require.NoError(t, err)
	require.Equal(t, "law", res.Focus.ID)
	depth := map[string]int{}
	for _, n := range res.Nodes {
		depth[n.Doc.ID] = n.Depth
	}
	require.Equal(t, map[string]int{"law": 0, "nd1": 1, "nd2": 1, "tt1": 2, "tt2": 2, "old": -1, "older": -2}, depth)
	edges := map[string]bool{}
	for _, e := range res.Edges {
		edges[e.From+">"+e.To+":"+e.Relation] = true
	}
	require.True(t, edges["nd1>law:guides"] && edges["nd1>law:based_on"] && edges["tt2>tt1:amends"] && edges["old>older:replaces"])
	require.False(t, edges["nd1>otherlaw:based_on"], "a node outside the map draws no edge")
	require.Len(t, res.Edges, 8)

	// depth 1: the circulars and the older law fall away
	res, err = svc.Explore(ctx, "law", 1)
	require.NoError(t, err)
	require.Len(t, res.Nodes, 4)

	// an unknown document
	_, err = svc.Explore(ctx, "nope", 2)
	require.ErrorIs(t, err, types.ErrLegalGraphFocusNotFound)
}

// The per-node fan cap keeps the strongest relations and counts the rest.
func TestLegalGraphExploreFanCap(t *testing.T) {
	db, svc := newLegalExploreFixture(t)
	exploreDoc(t, db, "law", "Luật")
	exploreDoc(t, db, "guide", "Nghị định")
	exploreEdge(t, db, "guide", "law", types.LegalRelationGuides)
	for i := 0; i < legalExploreBasisCap+5; i++ {
		id := fmt.Sprintf("nd%02d", i)
		exploreDoc(t, db, id, "Nghị định")
		exploreEdge(t, db, id, "law", types.LegalRelationBasedOn)
	}
	res, err := svc.Explore(context.Background(), "law", 1)
	require.NoError(t, err)
	require.Len(t, res.Nodes, 1+1+legalExploreBasisCap)
	var focus *types.LegalGraphExploreNode
	for _, n := range res.Nodes {
		if n.Depth == 0 {
			focus = n
		}
	}
	require.Equal(t, map[string]int{"based_on": 5}, focus.Dropped)
	require.False(t, res.NodeCapHit)
}
