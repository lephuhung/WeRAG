package service

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// copyOf writes knowledge kid with the text chunks given and, when status
// is not "", its link to doc in that review status.
func (f *legalTextFixture) copyOf(kid, doc string, status types.LegalReviewStatus, chunks ...string) {
	require.NoError(f.t, f.db.Create(&types.Knowledge{ID: kid, KnowledgeBaseID: "kb", Title: kid}).Error)
	if status != "" {
		require.NoError(f.t, f.db.Create(&types.LegalDocumentLink{KnowledgeID: kid, KnowledgeBaseID: "kb",
			LegalDocumentID: doc, ReviewStatus: status}).Error)
	}
	for i, c := range chunks {
		require.NoError(f.t, f.db.Create(&types.Chunk{ID: uuid.NewString(), KnowledgeID: kid, KnowledgeBaseID: "kb",
			Content: c, ChunkIndex: i, ChunkType: types.ChunkTypeText}).Error)
	}
}

// The parse hook reads the text of an approved copy only.
func TestLegalTextRelationsOfKnowledge(t *testing.T) {
	f := newLegalTextFixture(t)
	ctx := context.Background()
	f.doc("dcs-b", "dangcongsan", "24-QD/TW", "2021-07-30")
	f.doc("dcs-a", "dangcongsan", "117-QD/TW", "2023-08-18")
	f.doc("dcs-p", "dangcongsan", "118-QD/TW", "2023-08-19")
	cites := "- Căn cứ Quy định số 24-QĐ/TW ngày 30/7/2021;"
	f.copyOf("k-a", "dcs-a", types.LegalReviewApproved, cites)
	f.copyOf("k-p", "dcs-p", types.LegalReviewPending, cites)
	f.copyOf("k-none", "", "", cites)

	doc, stats, err := LegalTextRelationsOfKnowledge(ctx, f.db, "k-a")
	require.NoError(t, err)
	require.Equal(t, "dcs-a", doc)
	require.Equal(t, 1, stats.Relations)
	require.Equal(t, map[string]types.LegalRelation{"dcs-b": types.LegalRelationBasedOn}, f.rels("dcs-a"))

	for _, kid := range []string{"k-p", "k-none", "k-missing", ""} {
		doc, stats, err := LegalTextRelationsOfKnowledge(ctx, f.db, kid)
		require.NoError(t, err, kid)
		require.Empty(t, doc, kid)
		require.Nil(t, stats, kid)
	}
	require.Empty(t, f.rels("dcs-p"), "a pending copy is not read")
	doc, stats, err = LegalTextRelationsOfKnowledge(ctx, nil, "k-a")
	require.NoError(t, err)
	require.Empty(t, doc)
	require.Nil(t, stats)

	// the knowledge service runs it after processChunks, and without the
	// catalogue database it does nothing
	require.NoError(t, f.db.Where("from_id = ?", "dcs-a").Delete(&types.LegalDocumentRelation{}).Error)
	(&knowledgeService{}).legalTextRelationsAfterParse(ctx, "k-a")
	require.Empty(t, f.rels("dcs-a"))
	ks := &knowledgeService{}
	ks.SetLegalCatalogDB(f.db)
	ks.legalTextRelationsAfterParse(ctx, "k-a")
	require.Equal(t, map[string]types.LegalRelation{"dcs-b": types.LegalRelationBasedOn}, f.rels("dcs-a"))
}

// Approving a pending feed copy reads its text; re-linking it reads the new
// document's and re-reads the old one's, which, left without an approved
// copy, keeps its text edges (BuildLegalTextRelations counts it WithoutText).
func TestLegalReviewDecideReadsTextRelations(t *testing.T) {
	f := newLegalTextFixture(t)
	ctx := context.WithValue(context.Background(), types.UserIDContextKey, "admin-1")
	f.doc("dcs-b", "dangcongsan", "24-QD/TW", "2021-07-30")
	f.doc("dcs-a", "dangcongsan", "117-QD/TW", "2023-08-18")
	f.doc("dcs-c", "dangcongsan", "119-QD/TW", "2023-08-20")
	f.copyOf("k-a", "dcs-a", types.LegalReviewPending, "- Căn cứ Quy định số 24-QĐ/TW ngày 30/7/2021;")
	svc := NewLegalReviewService(repository.NewLegalCatalogRepository(f.db), repository.NewLegalReviewRepository(f.db),
		nil, nil)

	// without the database the decision reads nothing
	_, err := svc.Decide(ctx, "k-a", &types.LegalReviewDecision{Action: "approve", LegalDocumentID: "dcs-a"})
	require.NoError(t, err)
	require.Empty(t, f.rels("dcs-a"))

	svc.(interface{ SetLegalCatalogDB(*gorm.DB) }).SetLegalCatalogDB(f.db)
	_, err = svc.Decide(ctx, "k-a", &types.LegalReviewDecision{Action: "approve", LegalDocumentID: "dcs-a"})
	require.NoError(t, err)
	require.Equal(t, map[string]types.LegalRelation{"dcs-b": types.LegalRelationBasedOn}, f.rels("dcs-a"))

	_, err = svc.Decide(ctx, "k-a", &types.LegalReviewDecision{Action: "approve", LegalDocumentID: "dcs-c"})
	require.NoError(t, err)
	require.Equal(t, map[string]types.LegalRelation{"dcs-b": types.LegalRelationBasedOn}, f.rels("dcs-c"))
	require.Equal(t, map[string]types.LegalRelation{"dcs-b": types.LegalRelationBasedOn}, f.rels("dcs-a"),
		"a document left without an approved copy keeps its text edges")

	_, err = svc.Decide(ctx, "k-a", &types.LegalReviewDecision{Action: "reject"})
	require.NoError(t, err)
	require.Len(t, f.rels("dcs-c"), 1, "a rejected copy's document keeps its text edges too")
}
