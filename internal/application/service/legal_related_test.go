package service

import (
	"context"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestLegalCatalogRelated(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&types.KnowledgeBase{}, &types.Knowledge{},
		&types.LegalDocument{}, &types.LegalDocumentRelation{}, &types.LegalDocumentLink{}))
	ctx := context.Background()
	catalog := repository.NewLegalCatalogRepository(db)
	svc := NewLegalCatalogService(catalog, repository.NewLegalReviewRepository(db))

	day := func(s string) *time.Time { d, _ := time.Parse("2006-01-02", s); return &d }
	doc := func(id, num, typ, status, issued string) *types.LegalDocument {
		return &types.LegalDocument{ID: id, Source: types.LegalDocumentSourceVBPL, DocumentNumber: num,
			NumberKey: num, DocType: typ, Title: typ + " " + num, Status: status, IssuedOn: day(issued)}
	}
	require.NoError(t, catalog.UpsertDocuments(ctx, []*types.LegalDocument{
		doc("law-old", "04/2007/QH12", "Luật", "Hết hiệu lực toàn bộ", "2007-11-21"),
		doc("law", "109/2025/QH15", "Luật", "Còn hiệu lực", "2025-12-10"),
		doc("nd", "12/2026/NĐ-CP", "Nghị định", "Còn hiệu lực", "2026-01-15"),
		doc("nd-old", "65/2013/NĐ-CP", "Nghị định", "Hết hiệu lực toàn bộ", "2013-06-27"),
		doc("tt", "40/2026/TT-BTC", "Thông tư", "Còn hiệu lực", "2026-03-01"),
		doc("amend", "05/2026/QH16", "Luật", "Còn hiệu lực", "2026-04-24"),
	}))
	require.NoError(t, catalog.ReplaceRelations(ctx, []*types.LegalDocumentRelation{
		{FromID: "law", ToID: "law-old", Relation: types.LegalRelationReplaces},
		{FromID: "nd", ToID: "law", Relation: types.LegalRelationGuides},
		{FromID: "nd-old", ToID: "law", Relation: types.LegalRelationGuides},
		{FromID: "tt", ToID: "nd", Relation: types.LegalRelationGuides},
		{FromID: "amend", ToID: "law", Relation: types.LegalRelationAmends},
		{FromID: "tt", ToID: "law", Relation: types.LegalRelationBasedOn},
	}))
	// The decree is held by a public KB with an approved link.
	require.NoError(t, db.Create(&types.KnowledgeBase{ID: "kb", Name: "Nghị định", Visibility: types.KBVisibilityPublic}).Error)
	require.NoError(t, db.Create(&types.Knowledge{ID: "k-nd", KnowledgeBaseID: "kb", Title: "nd.html"}).Error)
	require.NoError(t, db.Create(&types.LegalDocumentLink{KnowledgeID: "k-nd", KnowledgeBaseID: "kb",
		LegalDocumentID: "nd", ReviewStatus: types.LegalReviewApproved}).Error)

	byID := func(rep *types.LegalRelatedReport) map[string]types.LegalRelatedDocument {
		out := map[string]types.LegalRelatedDocument{}
		for _, r := range rep.Related {
			out[r.Document.ID] = r
		}
		return out
	}

	// Guidance walks law ← decree ← circular and drops the expired decree.
	reports, err := svc.Related(ctx, "109/2025/QH15", "", types.LegalRelatedGuidance, 0, false)
	require.NoError(t, err)
	require.Len(t, reports, 1)
	got := byID(reports[0])
	require.Len(t, got, 2)
	require.Equal(t, 1, got["nd"].Depth)
	require.True(t, got["nd"].Incoming)
	require.Equal(t, "law", got["nd"].ParentID)
	require.Equal(t, []types.LegalKnowledgeRef{{KnowledgeID: "k-nd", KnowledgeBaseID: "kb",
		KnowledgeBaseName: "Nghị định", Title: "nd.html"}}, got["nd"].Knowledge)
	require.Equal(t, 2, got["tt"].Depth)
	require.Equal(t, "nd", got["tt"].ParentID)
	require.Empty(t, got["tt"].Knowledge)

	// One hop only, expired documents included.
	reports, err = svc.Related(ctx, "109/2025/QH15", "", types.LegalRelatedGuidance, 1, true)
	require.NoError(t, err)
	got = byID(reports[0])
	require.Len(t, got, 2)
	require.Contains(t, got, "nd-old")
	require.Equal(t, "nd", reports[0].Related[0].Document.ID, "documents in force come first")

	// History keeps the expired predecessor without being asked.
	reports, err = svc.Related(ctx, "109/2025/QH15", "", types.LegalRelatedHistory, 0, false)
	require.NoError(t, err)
	got = byID(reports[0])
	require.Len(t, got, 1)
	require.False(t, got["law-old"].Incoming, "the law replaces its predecessor")
	require.Equal(t, types.LegalRelationReplaces, got["law-old"].Relation)

	reports, err = svc.Related(ctx, "109/2025/QH15", "", types.LegalRelatedAmendments, 0, false)
	require.NoError(t, err)
	require.Contains(t, byID(reports[0]), "amend")

	// Basis walks up from the circular: to the decree it guides and the
	// law it is based on (reached first, so the decree's own edge to the
	// law does not repeat it).
	reports, err = svc.Related(ctx, "40/2026/TT-BTC", "", types.LegalRelatedBasis, 0, false)
	require.NoError(t, err)
	got = byID(reports[0])
	require.Len(t, got, 2)
	require.Equal(t, 1, got["law"].Depth)
	require.Equal(t, 1, got["nd"].Depth)

	_, err = svc.Related(ctx, "109/2025/QH15", "", "nonsense", 0, false)
	require.Error(t, err)

	reports, err = svc.Related(ctx, "1/2099/QH99", "", types.LegalRelatedGuidance, 0, false)
	require.NoError(t, err)
	require.Empty(t, reports)

	// Expansion from a search hit on the law: guidance two hops and the
	// amendment, expired documents included for the caller to name.
	require.NoError(t, db.Create(&types.Knowledge{ID: "k-law", KnowledgeBaseID: "kb", Title: "law.html"}).Error)
	require.NoError(t, db.Create(&types.LegalDocumentLink{KnowledgeID: "k-law", KnowledgeBaseID: "kb",
		LegalDocumentID: "law", ReviewStatus: types.LegalReviewApproved}).Error)
	exps, err := svc.ExpandRelated(ctx, []string{"k-unlinked", "k-law"})
	require.NoError(t, err)
	got = map[string]types.LegalRelatedDocument{}
	for _, e := range exps {
		require.Equal(t, "law", e.Anchor.ID)
		require.Equal(t, "k-law", e.AnchorKnowledgeID)
		got[e.Related.Document.ID] = e.Related
	}
	require.Len(t, got, 4)
	require.Equal(t, "k-nd", got["nd"].Knowledge[0].KnowledgeID)
	require.Equal(t, 2, got["tt"].Depth)
	require.Contains(t, got, "nd-old")
	require.Equal(t, types.LegalRelationAmends, got["amend"].Relation)

	// A hit on the old law reaches the law replacing it.
	require.NoError(t, db.Create(&types.Knowledge{ID: "k-law-old", KnowledgeBaseID: "kb", Title: "law-old.html"}).Error)
	require.NoError(t, db.Create(&types.LegalDocumentLink{KnowledgeID: "k-law-old", KnowledgeBaseID: "kb",
		LegalDocumentID: "law-old", ReviewStatus: types.LegalReviewApproved}).Error)
	exps, err = svc.ExpandRelated(ctx, []string{"k-law-old"})
	require.NoError(t, err)
	require.NotEmpty(t, exps)
	require.Equal(t, "law", exps[0].Related.Document.ID, "the successor comes first")
	require.Equal(t, types.LegalRelationReplaces, exps[0].Related.Relation)
	require.True(t, exps[0].Related.Incoming)

	exps, err = svc.ExpandRelated(ctx, []string{"k-unlinked"})
	require.NoError(t, err)
	require.Empty(t, exps, "only hits with an approved legal link are expanded")

	// KB graph: the KB holds the law and the decree; their neighbours come
	// along, and a based_on into the KB from outside is not drawn.
	view, err := svc.KBGraph(ctx, "kb", 0)
	require.NoError(t, err)
	nodes := map[string]*types.GraphViewNode{}
	for _, n := range view.Nodes {
		nodes[n.ID] = n
	}
	require.Len(t, nodes, 6, "law, decree + law-old, nd-old, tt, amend")
	require.Equal(t, "true", nodes["law"].Meta["in_kb"])
	require.Equal(t, "k-nd", nodes["nd"].Meta["knowledge_id"])
	require.Empty(t, nodes["tt"].Meta["in_kb"])
	require.Equal(t, "Luật 109/2025/QH15", nodes["law"].Name)
	require.Equal(t, "2025-12-10", nodes["law"].Meta["issued_on"])
	edges := map[string]bool{}
	for _, e := range view.Edges {
		edges[e.Source+" "+e.Type+" "+e.Target] = true
	}
	require.True(t, edges["tt guides nd"])
	require.True(t, edges["law replaces law-old"])
	require.False(t, edges["tt based_on law"], "a căn cứ an outside document states is not drawn")
	require.False(t, view.Truncated)

	view, err = svc.KBGraph(ctx, "kb", 3)
	require.NoError(t, err)
	require.Len(t, view.Nodes, 3)
	require.True(t, view.Truncated)
}

// A KB document's căn cứ and dẫn chiếu to documents outside the KB are
// drawn (outside documents as plain nodes); the budget ranks a strong edge
// above several weak ones.
func TestLegalKBGraphWeakEdgesOutside(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&types.KnowledgeBase{}, &types.Knowledge{},
		&types.LegalDocument{}, &types.LegalDocumentRelation{}, &types.LegalDocumentLink{}))
	ctx := context.Background()
	catalog := repository.NewLegalCatalogRepository(db)
	svc := NewLegalCatalogService(catalog, repository.NewLegalReviewRepository(db))
	var docs []*types.LegalDocument
	for _, id := range []string{"p-a", "p-x", "v-y", "p-z", "s", "w", "o1", "o2", "w1", "w2", "w3", "w4", "w5"} {
		docs = append(docs, &types.LegalDocument{ID: id, Source: "dangcongsan", DocumentNumber: id, NumberKey: id,
			DocType: "Quy định", Title: id})
	}
	require.NoError(t, catalog.UpsertDocuments(ctx, docs))
	rel := func(from, to string, r types.LegalRelation) *types.LegalDocumentRelation {
		return &types.LegalDocumentRelation{FromID: from, ToID: to, Relation: r, SourceLabel: LegalTextRelationLabel}
	}
	rels := []*types.LegalDocumentRelation{
		rel("p-a", "p-x", types.LegalRelationBasedOn),
		rel("p-a", "v-y", types.LegalRelationReferences),
		rel("p-z", "p-a", types.LegalRelationBasedOn), // stated by an outside document
		rel("s", "o1", types.LegalRelationAmends), rel("s", "o2", types.LegalRelationReplaces),
	}
	for _, w := range []string{"w1", "w2", "w3", "w4", "w5"} {
		rels = append(rels, rel("w", w, types.LegalRelationBasedOn))
	}
	require.NoError(t, db.Create(&rels).Error)
	hold := func(kb, doc string) {
		require.NoError(t, db.Create(&types.Knowledge{ID: "k-" + kb + doc, KnowledgeBaseID: kb, Title: doc}).Error)
		require.NoError(t, db.Create(&types.LegalDocumentLink{KnowledgeID: "k-" + kb + doc, KnowledgeBaseID: kb,
			LegalDocumentID: doc, ReviewStatus: types.LegalReviewApproved}).Error)
	}
	for _, kb := range []string{"party", "mixed"} {
		require.NoError(t, db.Create(&types.KnowledgeBase{ID: kb, Name: kb, Visibility: types.KBVisibilityPublic}).Error)
	}
	hold("party", "p-a")
	for _, d := range []string{"s", "w"} {
		hold("mixed", d)
	}

	view, err := svc.KBGraph(ctx, "party", 0)
	require.NoError(t, err)
	nodes := map[string]*types.GraphViewNode{}
	for _, n := range view.Nodes {
		nodes[n.ID] = n
	}
	require.Len(t, nodes, 3, "p-a and the two documents it cites")
	require.Equal(t, "true", nodes["p-a"].Meta["in_kb"])
	require.Empty(t, nodes["p-x"].Meta["in_kb"])
	require.Empty(t, nodes["v-y"].Meta["in_kb"])
	require.Equal(t, 2, nodes["p-a"].Degree)
	edges := map[string]bool{}
	for _, e := range view.Edges {
		edges[e.Source+" "+e.Type+" "+e.Target] = true
	}
	require.Equal(t, map[string]bool{"p-a based_on p-x": true, "p-a references v-y": true}, edges)

	// two nodes: w cites five căn cứ, s amends and replaces one document
	// each: s and a document it changes win the budget
	view, err = svc.KBGraph(ctx, "mixed", 2)
	require.NoError(t, err)
	require.True(t, view.Truncated)
	got := map[string]int{}
	for _, n := range view.Nodes {
		got[n.ID] = n.Degree
	}
	require.Len(t, got, 2)
	require.Equal(t, 2, got["s"])
	require.NotContains(t, got, "w")
	_, o1 := got["o1"]
	_, o2 := got["o2"]
	require.True(t, o1 || o2, "%v", got)
}
