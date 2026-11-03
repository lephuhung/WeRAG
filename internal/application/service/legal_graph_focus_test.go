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

func TestLegalGraphFocus(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&types.KnowledgeBase{}, &types.Knowledge{}, &types.LegalDocument{},
		&types.LegalDocumentRelation{}, &types.LegalDocumentLink{}, &types.LegalProvisionLink{}))
	ctx := context.Background()
	catalog := repository.NewLegalCatalogRepository(db)
	svc := NewLegalGraphFocusService(catalog, repository.NewLegalReviewRepository(db), db)

	day := func(s string) *time.Time { d, _ := time.Parse("2006-01-02", s); return &d }
	doc := func(id, num, typ, issued string) *types.LegalDocument {
		return &types.LegalDocument{ID: id, Source: types.LegalDocumentSourceVBPL, DocumentNumber: num,
			NumberKey: num, DocType: typ, Title: typ + " về đất đai " + num, Status: "Còn hiệu lực", IssuedOn: day(issued)}
	}
	require.NoError(t, catalog.UpsertDocuments(ctx, []*types.LegalDocument{
		doc("law", "31/2024/QH15", "Luật", "2024-01-18"),
		doc("law-old", "45/2013/QH13", "Luật", "2013-11-29"),
		doc("amend", "43/2024/QH15", "Luật", "2024-06-29"),
		doc("nd", "102/2024/NĐ-CP", "Nghị định", "2024-07-30"),
		doc("const", "HP/2013", "Hiến pháp", "2013-11-28"),
		doc("prov-only", "15/2025/QH15", "Luật", "2025-02-19"),
		doc("far", "01/2026/TT-BTNMT", "Thông tư", "2026-01-05"),
		doc("other", "99/2025/QH15", "Luật", "2025-06-01"),
	}))
	require.NoError(t, catalog.ReplaceRelations(ctx, []*types.LegalDocumentRelation{
		{FromID: "law", ToID: "law-old", Relation: types.LegalRelationReplaces},
		{FromID: "amend", ToID: "law", Relation: types.LegalRelationAmends},
		{FromID: "nd", ToID: "law", Relation: types.LegalRelationGuides},
		{FromID: "nd", ToID: "law", Relation: types.LegalRelationBasedOn},
		{FromID: "law", ToID: "const", Relation: types.LegalRelationBasedOn},
		// two hops from the KB: not focusable
		{FromID: "far", ToID: "nd", Relation: types.LegalRelationGuides},
	}))
	require.NoError(t, db.Create(&types.KnowledgeBase{ID: "kb", Name: "Đất đai"}).Error)
	for _, k := range []struct{ kid, doc string }{{"k-law", "law"}, {"k-other", "other"}} {
		require.NoError(t, db.Create(&types.Knowledge{ID: k.kid, KnowledgeBaseID: "kb", Title: k.doc + ".html"}).Error)
		require.NoError(t, db.Create(&types.LegalDocumentLink{KnowledgeID: k.kid, KnowledgeBaseID: "kb",
			LegalDocumentID: k.doc, ReviewStatus: types.LegalReviewApproved}).Error)
	}
	links := []types.LegalProvisionLink{
		{TargetDocID: "law", TargetProvisionID: "p1", TargetLevel: "clause", TargetPath: "dieu:10/khoan:2",
			RelationCode: 10, Op: "sua_doi_bo_sung", SourceDocID: "amend", SourceProvisionID: "s1", SourcePath: "dieu:1/khoan:3", EffFrom: day("2024-08-01")},
		{TargetDocID: "law", TargetProvisionID: "p2", TargetLevel: "article", TargetPath: "dieu:2",
			RelationCode: 1, Op: "bai_bo", SourceDocID: "prov-only", SourceProvisionID: "s2", SourcePath: "dieu:5"},
		{TargetDocID: "law-old", TargetProvisionID: "p3", TargetLevel: "article", TargetPath: "dieu:7",
			RelationCode: 12, Op: "thay_the", SourceDocID: "law", SourceProvisionID: "s3", SourcePath: "dieu:260"},
	}
	require.NoError(t, db.Create(&links).Error)

	// Default focus: the best-connected held document.
	f, err := svc.KBFocus(ctx, "kb", "", "")
	require.NoError(t, err)
	require.Equal(t, "law", f.Focus.ID)
	require.True(t, f.Focus.InKB)
	require.Equal(t, "k-law", f.Focus.KnowledgeID)
	require.Equal(t, 2, f.AnchorCount)
	require.Len(t, f.Candidates, 2)
	require.Equal(t, "law", f.Candidates[0].ID)

	nb := map[string]*types.LegalGraphNeighbour{}
	for _, n := range f.Neighbours {
		nb[n.Direction+" "+n.Doc.ID] = n
	}
	require.Equal(t, []string{"replaces"}, nb["out law-old"].Relations)
	require.Equal(t, []string{"based_on"}, nb["out const"].Relations)
	require.Equal(t, []string{"amends"}, nb["in amend"].Relations)
	require.False(t, nb["in amend"].FromProvisions, "the catalogue relation wins")
	require.Equal(t, []string{"guides", "based_on"}, nb["in nd"].Relations, "strongest first")
	require.Equal(t, []string{"repeals"}, nb["in prov-only"].Relations)
	require.True(t, nb["in prov-only"].FromProvisions)
	require.Len(t, nb, 5)
	require.Equal(t, 1, f.NeighbourTotals["in:guides"])

	require.Len(t, f.ProvisionsIn, 2)
	require.EqualValues(t, 2, f.ProvisionsInTotal)
	require.Len(t, f.ProvisionsOut, 1)
	require.Equal(t, "law-old", f.ProvisionsOut[0].DocID)
	require.Equal(t, "dieu:7", f.ProvisionsOut[0].TargetPath)
	require.Equal(t, "2024-08-01", f.ProvisionsIn[0].EffFrom)
	require.Equal(t, "43/2024/QH15", f.Documents["amend"].DocumentNumber)
	require.False(t, f.ProvisionsTruncated)

	// A neighbour can be focused; its own neighbours two hops out cannot.
	f, err = svc.KBFocus(ctx, "kb", "nd", "")
	require.NoError(t, err)
	require.Equal(t, "nd", f.Focus.ID)
	require.False(t, f.Focus.InKB)
	require.True(t, f.Focus.Focusable)
	for _, n := range f.Neighbours {
		if n.Doc.ID == "far" {
			require.False(t, n.Doc.Focusable)
		}
		if n.Doc.ID == "law" {
			require.True(t, n.Doc.Focusable)
		}
	}
	// A document linked only by provisions is a neighbour too.
	_, err = svc.KBFocus(ctx, "kb", "prov-only", "")
	require.NoError(t, err)

	// Beyond one hop, or unknown: not found.
	_, err = svc.KBFocus(ctx, "kb", "far", "")
	require.ErrorIs(t, err, types.ErrLegalGraphFocusNotFound)
	_, err = svc.KBFocus(ctx, "kb", "nope", "")
	require.ErrorIs(t, err, types.ErrLegalGraphFocusNotFound)
	_, err = svc.KBFocus(ctx, "other-kb", "law", "")
	require.ErrorIs(t, err, types.ErrLegalGraphFocusNotFound)

	// Picker search: number or title, diacritics optional.
	f, err = svc.KBFocus(ctx, "kb", "", "99/2025")
	require.NoError(t, err)
	require.Len(t, f.Candidates, 1)
	require.Equal(t, "other", f.Candidates[0].ID)
	f, err = svc.KBFocus(ctx, "kb", "", "luat dat dai 31")
	require.NoError(t, err)
	require.Len(t, f.Candidates, 1)
	require.Equal(t, "law", f.Candidates[0].ID)

	// An empty KB has nothing to focus.
	f, err = svc.KBFocus(ctx, "empty", "", "")
	require.NoError(t, err)
	require.Nil(t, f.Focus)
	require.Zero(t, f.AnchorCount)
}
