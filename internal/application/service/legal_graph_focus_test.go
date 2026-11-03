package service

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type legalFocusFixture struct {
	t       *testing.T
	ctx     context.Context
	db      *gorm.DB
	catalog interfaces.LegalCatalogRepository
	svc     interfaces.LegalGraphFocusService
}

func newLegalFocusFixture(t *testing.T) *legalFocusFixture {
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&types.KnowledgeBase{}, &types.Knowledge{}, &types.LegalDocument{},
		&types.LegalDocumentRelation{}, &types.LegalDocumentLink{}, &types.LegalProvisionLink{}))
	catalog := repository.NewLegalCatalogRepository(db)
	return &legalFocusFixture{t: t, ctx: context.Background(), db: db, catalog: catalog,
		svc: NewLegalGraphFocusService(catalog, db)}
}

func legalFocusDay(s string) *time.Time { d, _ := time.Parse("2006-01-02", s); return &d }

func (f *legalFocusFixture) docs(docs ...*types.LegalDocument) {
	require.NoError(f.t, f.catalog.UpsertDocuments(f.ctx, docs))
}

func legalFocusDoc(id, num, typ, issued string) *types.LegalDocument {
	return &types.LegalDocument{ID: id, Source: types.LegalDocumentSourceVBPL, DocumentNumber: num,
		NumberKey: num, DocType: typ, Title: typ + " về đất đai " + num, Status: "Còn hiệu lực", IssuedOn: legalFocusDay(issued)}
}

func (f *legalFocusFixture) kb(id string) {
	require.NoError(f.t, f.db.Create(&types.KnowledgeBase{ID: id, Name: id}).Error)
}

// hold links knowledge kid of kb to doc with status.
func (f *legalFocusFixture) hold(kb, kid, doc string, status types.LegalReviewStatus) {
	require.NoError(f.t, f.db.Create(&types.Knowledge{ID: kid, KnowledgeBaseID: kb, Title: doc + ".html"}).Error)
	require.NoError(f.t, f.db.Create(&types.LegalDocumentLink{KnowledgeID: kid, KnowledgeBaseID: kb,
		LegalDocumentID: doc, ReviewStatus: status}).Error)
}

func TestLegalGraphFocus(t *testing.T) {
	f := newLegalFocusFixture(t)
	ctx, svc, day := f.ctx, f.svc, legalFocusDay
	doc := legalFocusDoc
	f.docs(
		doc("law", "31/2024/QH15", "Luật", "2024-01-18"),
		doc("law-old", "45/2013/QH13", "Luật", "2013-11-29"),
		doc("amend", "43/2024/QH15", "Luật", "2024-06-29"),
		doc("nd", "102/2024/NĐ-CP", "Nghị định", "2024-07-30"),
		doc("const", "HP/2013", "Hiến pháp", "2013-11-28"),
		doc("prov-only", "15/2025/QH15", "Luật", "2025-02-19"),
		doc("far", "01/2026/TT-BTNMT", "Thông tư", "2026-01-05"),
		doc("other", "99/2025/QH15", "Luật", "2025-06-01"),
	)
	require.NoError(t, f.catalog.ReplaceRelations(ctx, []*types.LegalDocumentRelation{
		{FromID: "law", ToID: "law-old", Relation: types.LegalRelationReplaces},
		{FromID: "amend", ToID: "law", Relation: types.LegalRelationAmends},
		{FromID: "nd", ToID: "law", Relation: types.LegalRelationGuides},
		{FromID: "nd", ToID: "law", Relation: types.LegalRelationBasedOn},
		{FromID: "law", ToID: "const", Relation: types.LegalRelationBasedOn},
		// two hops from the KB: not focusable
		{FromID: "far", ToID: "nd", Relation: types.LegalRelationGuides},
	}))
	f.kb("kb")
	f.hold("kb", "k-law", "law", types.LegalReviewApproved)
	f.hold("kb", "k-other", "other", types.LegalReviewApproved)
	links := []types.LegalProvisionLink{
		{TargetDocID: "law", TargetProvisionID: "p1", TargetLevel: "clause", TargetPath: "dieu:10/khoan:2",
			RelationCode: 10, Op: "sua_doi_bo_sung", SourceDocID: "amend", SourceProvisionID: "s1", SourcePath: "dieu:1/khoan:3", EffFrom: day("2024-08-01")},
		{TargetDocID: "law", TargetProvisionID: "p2", TargetLevel: "article", TargetPath: "dieu:2",
			RelationCode: 1, Op: "bai_bo", SourceDocID: "prov-only", SourceProvisionID: "s2", SourcePath: "dieu:5"},
		{TargetDocID: "law-old", TargetProvisionID: "p3", TargetLevel: "article", TargetPath: "dieu:7",
			RelationCode: 12, Op: "thay_the", SourceDocID: "law", SourceProvisionID: "s3", SourcePath: "dieu:260"},
	}
	require.NoError(t, f.db.Create(&links).Error)

	// Default focus: the best-connected held document.
	g, err := svc.KBFocus(ctx, "kb", "", "")
	require.NoError(t, err)
	require.Equal(t, "law", g.Focus.ID)
	require.True(t, g.Focus.InKB)
	require.Equal(t, "k-law", g.Focus.KnowledgeID)
	require.Equal(t, 2, g.AnchorCount)
	require.Len(t, g.Candidates, 2)
	require.Equal(t, "law", g.Candidates[0].ID)
	require.Equal(t, 3, g.Candidates[0].Degree, "replaces + amends + guides; căn cứ not counted")

	nb := map[string]*types.LegalGraphNeighbour{}
	for _, n := range g.Neighbours {
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
	require.Equal(t, 1, g.NeighbourTotals["in:guides"])
	require.False(t, g.NeighboursTruncated)

	require.Len(t, g.ProvisionsIn, 2)
	require.EqualValues(t, 2, g.ProvisionsInTotal)
	require.Len(t, g.ProvisionsOut, 1)
	require.Equal(t, "law-old", g.ProvisionsOut[0].DocID)
	require.Equal(t, "dieu:7", g.ProvisionsOut[0].TargetPath)
	require.Equal(t, "2024-08-01", g.ProvisionsIn[0].EffFrom)
	require.Equal(t, "43/2024/QH15", g.Documents["amend"].DocumentNumber)
	require.False(t, g.ProvisionsTruncated)

	// A neighbour can be focused; its own neighbours two hops out cannot.
	g, err = svc.KBFocus(ctx, "kb", "nd", "")
	require.NoError(t, err)
	require.Equal(t, "nd", g.Focus.ID)
	require.False(t, g.Focus.InKB)
	require.True(t, g.Focus.Focusable)
	for _, n := range g.Neighbours {
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

	// Picker search: number or title, diacritics and case optional.
	g, err = svc.KBFocus(ctx, "kb", "", "99/2025")
	require.NoError(t, err)
	require.Len(t, g.Candidates, 1)
	require.Equal(t, "other", g.Candidates[0].ID)
	require.Nil(t, g.Focus, "a picker search reads the candidates only")
	for _, q := range []string{"luat dat dai 31", "LUẬT ĐẤT ĐAI 31", "Luật Ðất đai 31"} {
		g, err = svc.KBFocus(ctx, "kb", "", q)
		require.NoError(t, err)
		require.Len(t, g.Candidates, 1, q)
		require.Equal(t, "law", g.Candidates[0].ID, q)
	}
	g, err = svc.KBFocus(ctx, "kb", "", "102/2024")
	require.NoError(t, err)
	require.Empty(t, g.Candidates, "only held documents are candidates")
	g, err = svc.KBFocus(ctx, "kb", "", "50%")
	require.NoError(t, err)
	require.Empty(t, g.Candidates, "LIKE wildcards are literal")

	// A focus request without q reads no candidates (the picker keeps the
	// first load's).
	g, err = svc.KBFocus(ctx, "kb", "law", "")
	require.NoError(t, err)
	require.Empty(t, g.Candidates)
	// A long q is cut to 10 words: the junk after them is ignored.
	g, err = svc.KBFocus(ctx, "kb", "", "luat ve dat dai 31/2024/qh15 luat ve dat dai luat zzz zzz zzz")
	require.NoError(t, err)
	require.Len(t, g.Candidates, 1)
	require.Equal(t, "law", g.Candidates[0].ID)

	// An empty KB has nothing to focus.
	g, err = svc.KBFocus(ctx, "empty", "", "")
	require.NoError(t, err)
	require.Nil(t, g.Focus)
	require.Zero(t, g.AnchorCount)
}

func TestLegalGraphFocusAnchorsMustBeLive(t *testing.T) {
	f := newLegalFocusFixture(t)
	ctx := f.ctx
	f.docs(
		legalFocusDoc("ok", "01/2024/QH15", "Luật", "2024-01-01"),
		legalFocusDoc("pending", "02/2024/QH15", "Luật", "2024-01-02"),
		legalFocusDoc("rejected", "03/2024/QH15", "Luật", "2024-01-03"),
		legalFocusDoc("gone-k", "04/2024/QH15", "Luật", "2024-01-04"),
		legalFocusDoc("gone-kb", "05/2024/QH15", "Luật", "2024-01-05"),
		legalFocusDoc("next-to-pending", "06/2024/NĐ-CP", "Nghị định", "2024-01-06"),
	)
	require.NoError(t, f.catalog.ReplaceRelations(ctx, []*types.LegalDocumentRelation{
		{FromID: "next-to-pending", ToID: "pending", Relation: types.LegalRelationGuides},
	}))
	f.kb("kb")
	f.kb("kb-dead")
	f.hold("kb", "k-ok", "ok", types.LegalReviewApproved)
	f.hold("kb", "k-pending", "pending", types.LegalReviewPending)
	f.hold("kb", "k-rejected", "rejected", types.LegalReviewRejected)
	f.hold("kb", "k-gone", "gone-k", types.LegalReviewApproved)
	require.NoError(t, f.db.Delete(&types.Knowledge{ID: "k-gone"}).Error)
	f.hold("kb-dead", "k-dead", "gone-kb", types.LegalReviewApproved)
	require.NoError(t, f.db.Delete(&types.KnowledgeBase{ID: "kb-dead"}).Error)

	g, err := f.svc.KBFocus(ctx, "kb", "", "")
	require.NoError(t, err)
	require.Equal(t, 1, g.AnchorCount)
	require.Equal(t, "ok", g.Focus.ID)
	require.Len(t, g.Candidates, 1)
	for _, id := range []string{"pending", "rejected", "gone-k", "next-to-pending"} {
		_, err := f.svc.KBFocus(ctx, "kb", id, "")
		require.ErrorIs(t, err, types.ErrLegalGraphFocusNotFound, id)
	}
	g, err = f.svc.KBFocus(ctx, "kb-dead", "", "")
	require.NoError(t, err)
	require.Zero(t, g.AnchorCount, "a deleted KB holds nothing")
	_, err = f.svc.KBFocus(ctx, "kb-dead", "gone-kb", "")
	require.ErrorIs(t, err, types.ErrLegalGraphFocusNotFound)
	g, err = f.svc.KBFocus(ctx, "kb", "", "02/2024")
	require.NoError(t, err)
	require.Empty(t, g.Candidates, "a pending link is no candidate")
}

// TestLegalGraphFocusCaps covers a KB larger than the old 500-anchor read,
// the 20 candidates, the 60 neighbours per group and the 600 provision rows.
func TestLegalGraphFocusCaps(t *testing.T) {
	f := newLegalFocusFixture(t)
	ctx := f.ctx
	const anchors = 650
	docs := []*types.LegalDocument{legalFocusDoc("hub", "100/2020/QH14", "Luật", "2020-06-01")}
	var rels []*types.LegalDocumentRelation
	for i := 0; i < anchors; i++ {
		id := fmt.Sprintf("a%03d", i)
		docs = append(docs, legalFocusDoc(id, fmt.Sprintf("%d/2021/NĐ-CP", i), "Nghị định", "2021-01-01"))
		rels = append(rels, &types.LegalDocumentRelation{FromID: id, ToID: "hub", Relation: types.LegalRelationGuides})
	}
	// The last anchor (linked last: oldest of an updated_at DESC read) has
	// its own outside neighbour.
	docs = append(docs, legalFocusDoc("outside", "7/2022/TT-BTC", "Thông tư", "2022-02-02"))
	rels = append(rels, &types.LegalDocumentRelation{FromID: "outside", ToID: "a000", Relation: types.LegalRelationGuides})
	f.docs(docs...)
	require.NoError(t, f.catalog.ReplaceRelations(ctx, rels))
	f.kb("big")
	for i := anchors - 1; i >= 0; i-- {
		id := fmt.Sprintf("a%03d", i)
		f.hold("big", "k-"+id, id, types.LegalReviewApproved)
	}
	var plinks []types.LegalProvisionLink
	for i := 0; i < 640; i++ {
		plinks = append(plinks, types.LegalProvisionLink{TargetDocID: "hub", TargetProvisionID: fmt.Sprintf("t%d", i),
			TargetLevel: "article", TargetPath: fmt.Sprintf("dieu:%d", i+1), RelationCode: 10, Op: "sua_doi_bo_sung",
			SourceDocID: "a001", SourceProvisionID: fmt.Sprintf("s%d", i), SourcePath: "dieu:1"})
	}
	require.NoError(t, f.db.CreateInBatches(&plinks, 200).Error)

	g, err := f.svc.KBFocus(ctx, "big", "", "")
	require.NoError(t, err)
	require.Equal(t, anchors, g.AnchorCount)
	require.Len(t, g.Candidates, legalFocusCandidates)
	require.Equal(t, "a000", g.Candidates[0].ID, "two relations; every other anchor has one")
	require.Equal(t, 2, g.Candidates[0].Degree)
	require.Equal(t, "a000", g.Focus.ID)

	// Every anchor is reachable, not only the newest 500.
	g, err = f.svc.KBFocus(ctx, "big", "a000", "")
	require.NoError(t, err)
	require.True(t, g.Focus.InKB)
	var outside *types.LegalGraphNeighbour
	for _, n := range g.Neighbours {
		if n.Doc.ID == "outside" {
			outside = n
		}
	}
	require.NotNil(t, outside)
	require.True(t, outside.Doc.Focusable)
	_, err = f.svc.KBFocus(ctx, "big", "outside", "")
	require.NoError(t, err)
	g, err = f.svc.KBFocus(ctx, "big", "", "649/2021")
	require.NoError(t, err)
	require.Len(t, g.Candidates, 1)
	require.Equal(t, "a649", g.Candidates[0].ID)

	// The hub: 650 guiding documents → 60 shown, all counted; 640 rows →
	// 600 read, all counted.
	g, err = f.svc.KBFocus(ctx, "big", "hub", "")
	require.NoError(t, err)
	require.False(t, g.Focus.InKB)
	require.Equal(t, anchors, g.NeighbourTotals["in:guides"])
	require.Len(t, g.Neighbours, legalFocusGroupCap)
	require.True(t, g.NeighboursTruncated)
	for _, n := range g.Neighbours {
		require.True(t, n.Doc.InKB)
		require.True(t, n.Doc.Focusable)
	}
	require.Len(t, g.ProvisionsIn, legalFocusProvisionCap)
	require.EqualValues(t, 640, g.ProvisionsInTotal)
	require.True(t, g.ProvisionsTruncated)
	require.Equal(t, "100/2020/QH14", g.Focus.DocumentNumber)
	require.Contains(t, g.Documents, "a001", "a provision row's document is loaded first")
}

// TestLegalFoldRegex checks the Postgres search pattern with Go's regexp
// (the same classes and escapes in both dialects of regex).
func TestLegalFoldRegex(t *testing.T) {
	require.Equal(t, "[dđð][iìíỉĩị]nh", legalFoldRegex("dinh"))
	require.Equal(t, `74\/2022`, legalFoldRegex("74/2022"))
	for _, w := range []string{"nghi", "dinh", "dat", "dai", "74/2022/tt-btc", "quyet"} {
		re := regexp.MustCompile(legalFoldRegex(w))
		hay := "nghị định về đất đai; thông tư 74/2022/tt-btc; quyết ðịnh"
		require.True(t, re.MatchString(hay), w)
	}
	require.False(t, regexp.MustCompile(legalFoldRegex("5.0")).MatchString("530"), "a dot is literal")
	require.Equal(t, "nghi dinh dat dai", legalFoldText("NGHỊ ĐỊNH Ðất đai"))
}

// TestLegalGraphFocusLoadCap: a document with more neighbours than the
// load cap still resolves every provision row's document, and the "căn
// cứ" crowd is what the cap drops.
func TestLegalGraphFocusLoadCap(t *testing.T) {
	f := newLegalFocusFixture(t)
	ctx := f.ctx
	docs := []*types.LegalDocument{
		legalFocusDoc("hub", "76/2015/QH13", "Luật", "2015-06-19"),
		legalFocusDoc("row-src", "1/2016/QH14", "Luật", "2016-01-01"),
		legalFocusDoc("amender", "2/2017/QH14", "Luật", "2017-01-01"),
	}
	rels := []*types.LegalDocumentRelation{{FromID: "amender", ToID: "hub", Relation: types.LegalRelationAmends}}
	for i := 0; i < legalFocusMaxDocs+100; i++ {
		id := fmt.Sprintf("b%04d", i)
		docs = append(docs, legalFocusDoc(id, fmt.Sprintf("%d/2020/QĐ-UBND", i), "Quyết định", "2020-01-01"))
		rels = append(rels, &types.LegalDocumentRelation{FromID: id, ToID: "hub", Relation: types.LegalRelationBasedOn})
	}
	f.docs(docs...)
	require.NoError(t, f.catalog.ReplaceRelations(ctx, rels))
	f.kb("kb")
	f.hold("kb", "k-hub", "hub", types.LegalReviewApproved)
	require.NoError(t, f.db.Create(&types.LegalProvisionLink{TargetDocID: "hub", TargetProvisionID: "t1", TargetLevel: "article",
		TargetPath: "dieu:1", RelationCode: 1, Op: "bai_bo", SourceDocID: "row-src", SourceProvisionID: "s1", SourcePath: "dieu:9"}).Error)

	g, err := f.svc.KBFocus(ctx, "kb", "hub", "")
	require.NoError(t, err)
	require.Contains(t, g.Documents, "row-src")
	require.Equal(t, legalFocusMaxDocs+100, g.NeighbourTotals["in:based_on"], "totals count before the cap")
	require.True(t, g.NeighboursTruncated)
	got := map[string]bool{}
	for _, n := range g.Neighbours {
		got[n.Doc.ID] = true
	}
	require.True(t, got["amender"], "a strong relation outranks the căn cứ crowd")
	require.True(t, got["row-src"])
}

func TestLegalSearchWords(t *testing.T) {
	require.Equal(t, []string{"nghi", "dinh", "15"}, legalSearchWords("  Nghị  ĐỊNH 15 "))
	many := strings.Repeat("a ", 30)
	require.Len(t, legalSearchWords(many), legalSearchMaxWords)
	long := strings.Repeat("ộ", 500)
	words := legalSearchWords(long)
	require.Len(t, words, 1)
	require.Len(t, []rune(words[0]), legalSearchMaxRunes)
	require.Empty(t, legalSearchWords("   "))
}
