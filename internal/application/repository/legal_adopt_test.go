package repository

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/Tencent/WeKnora/internal/legalcatalog"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/vietnamese_legal"
)

func legalAdoptTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&types.LegalDocument{}, &types.LegalDocumentRelation{}, &types.LegalDocumentLink{},
		&types.LegalDocumentRequest{}, &types.LegalDocumentDraft{}, &types.LegalProvisionLink{}, &types.LegalProvisionLinkCandidate{},
		&types.KnowledgeBase{}, &types.Knowledge{}, &types.Chunk{}))
	return db
}

func TestAdoptLegalDocument(t *testing.T) {
	db := legalAdoptTestDB(t)
	ctx := context.Background()
	day := func(s string) *time.Time { d, _ := time.Parse("2006-01-02", s); return &d }
	key := vietnamese_legal.CatalogNumberKey("777/2026/NĐ-CP")
	reviewed := time.Now()
	require.NoError(t, db.Create(&[]types.LegalDocument{
		{ID: "tvpl-777", Source: "tvpl", DocumentNumber: "777/2026/NĐ-CP", NumberKey: key, DocType: "Nghị định",
			Title: "Nghị định 777/2026/NĐ-CP quy định thuế", IssuedOn: day("2026-11-05"), Status: "Chưa xác định",
			ReviewedBy: "sa", ReviewedAt: &reviewed},
		{ID: "v777", Source: "vbpl", DocumentNumber: "777/2026/NĐ-CP", NumberKey: key, DocType: "Nghị định",
			Title: "Nghị định số 777/2026/NĐ-CP Quy định thuế", IssuedOn: day("2026-11-05"), Status: "Còn hiệu lực"},
		{ID: "nd68", Source: "vbpl", DocumentNumber: "68/2026/NĐ-CP", NumberKey: "68/2026/ND-CP"},
	}).Error)

	// the KB copy
	require.NoError(t, db.Create(&[]types.KnowledgeBase{{ID: "kb-pub", Name: "VBPL – Nghị định"},
		{ID: "kb-ten", Name: "tenant", OwnerTenantID: 7}}).Error)
	km, _ := json.Marshal(map[string]string{"external_id": "tvpl-777", "source": "tvpl"})
	require.NoError(t, db.Create(&[]types.Knowledge{{ID: "k1", KnowledgeBaseID: "kb-pub", Metadata: km},
		{ID: "k2", KnowledgeBaseID: "kb-ten", Metadata: km}}).Error)
	cm, _ := json.Marshal(types.DocumentChunkMetadata{Legal: &vietnamese_legal.LegalMetadata{Source: "tvpl", SourceDocID: "tvpl-777", ArticleNos: []string{"1"}}})
	require.NoError(t, db.Create(&[]types.Chunk{{ID: "c1", KnowledgeID: "k1", KnowledgeBaseID: "kb-pub", Content: "Điều 1.", Metadata: cm},
		{ID: "c2", KnowledgeID: "k2", KnowledgeBaseID: "kb-ten", Content: "Điều 1.", Metadata: cm}}).Error)
	require.NoError(t, db.Create(&types.LegalDocumentLink{KnowledgeID: "k1", LegalDocumentID: "tvpl-777"}).Error)

	// requests: one only for the feed row, one of a number filed for both
	now := time.Now()
	require.NoError(t, db.Create(&[]types.LegalDocumentRequest{
		{ID: "r1", NumberKey: key, DocumentNumber: "777/2026/NĐ-CP", LegalDocumentID: "tvpl-777", Status: "added", FirstRequestedAt: now, LastRequestedAt: now},
		{ID: "r2", NumberKey: "dup", DocumentNumber: "dup", LegalDocumentID: "tvpl-777", Status: "requested", FirstRequestedAt: now, LastRequestedAt: now},
		{ID: "r3", NumberKey: "dup", DocumentNumber: "dup", LegalDocumentID: "v777", Status: "requested", FirstRequestedAt: now, LastRequestedAt: now},
	}).Error)
	require.NoError(t, db.Create(&[]types.LegalDocumentRelation{
		{FromID: "nd68", ToID: "tvpl-777", Relation: types.LegalRelationReplaces},
		{FromID: "tvpl-777", ToID: "v777", Relation: types.LegalRelationReplaces}, // becomes a self edge
	}).Error)

	// provision links: 777 amends NĐ 68 (777 is B), and a link on 777 (A)
	// that is the same edge as one stored for v777 → merged
	fpA := legalcatalog.SyntheticProvisionID("tvpl-777", "dieu:1")
	fpB := legalcatalog.SyntheticProvisionID("tvpl-777", "dieu:2/khoan:1")
	fpA2 := legalcatalog.SyntheticProvisionID("v777", "dieu:1")
	links := []types.LegalProvisionLink{
		{TargetDocID: "nd68", TargetProvisionID: "p68-3", TargetPath: "dieu:3", RelationCode: 10, SourceDocID: "tvpl-777",
			SourceProvisionID: fpB, SourcePath: "dieu:2/khoan:1", Source: "tvpl"},
		{TargetDocID: "tvpl-777", TargetProvisionID: fpA, TargetPath: "dieu:1", RelationCode: 1, SourceDocID: "nd68",
			SourceProvisionID: "p68-9", SourcePath: "dieu:9", Source: "tvpl"},
		{TargetDocID: "v777", TargetProvisionID: fpA2, TargetPath: "dieu:1", RelationCode: 1, SourceDocID: "nd68",
			SourceProvisionID: "p68-9", SourcePath: "dieu:9", Source: "editor"},
	}
	require.NoError(t, db.Create(&links).Error)
	mergedID := links[1].ID
	markKey := func(amender string) types.JSON {
		b, _ := json.Marshal(map[string]string{"mark_key": "dieu:3|10|" + key + "|" + amender + "|dieu:2/khoan:1"})
		return b
	}
	cands := []types.LegalProvisionLinkCandidate{
		{Feed: "tvpl", FeedDocID: "68", TargetDocID: "nd68", TargetProvisionID: "p68-3", TargetPath: "dieu:3", RelationCode: 10,
			AmenderDocID: "tvpl-777", SourceProvisionID: fpB, SourcePath: "dieu:2/khoan:1", MatchStatus: "matched",
			ReviewStatus: "approved", Detail: markKey("tvpl-777")},
		{Feed: "tvpl", FeedDocID: "777", TargetDocID: "tvpl-777", TargetProvisionID: fpA, TargetPath: "dieu:1", RelationCode: 1,
			AmenderDocID: "nd68", SourceProvisionID: "p68-9", SourcePath: "dieu:9", MatchStatus: "matched",
			ReviewStatus: "approved", LinkID: &mergedID},
	}
	require.NoError(t, db.Create(&cands).Error)

	res, err := AdoptLegalDocument(ctx, db, "tvpl-777", "v777")
	require.NoError(t, err)
	require.Equal(t, []string{"k1"}, res.KnowledgeIDs, "only the public KB copy")
	require.EqualValues(t, 1, res.Chunks)
	require.True(t, res.ReviewInherited)

	var k1, k2 types.Knowledge
	require.NoError(t, db.First(&k1, "id = ?", "k1").Error)
	require.NoError(t, db.First(&k2, "id = ?", "k2").Error)
	require.Contains(t, string(k1.Metadata), `"external_id":"v777"`)
	require.Contains(t, string(k2.Metadata), `"external_id":"tvpl-777"`, "a tenant KB is left alone")
	var c1 types.Chunk
	require.NoError(t, db.First(&c1, "id = ?", "c1").Error)
	var meta types.DocumentChunkMetadata
	require.NoError(t, json.Unmarshal(c1.Metadata, &meta))
	require.Equal(t, "v777", meta.Legal.SourceDocID)

	var link types.LegalDocumentLink
	require.NoError(t, db.First(&link, "knowledge_id = ?", "k1").Error)
	require.Equal(t, "v777", link.LegalDocumentID)

	var reqs []types.LegalDocumentRequest
	require.NoError(t, db.Order("id").Find(&reqs).Error)
	require.Len(t, reqs, 2, "the feed row's request of a number v777 already has is dropped")
	for _, r := range reqs {
		require.Equal(t, "v777", r.LegalDocumentID)
	}

	var rels []types.LegalDocumentRelation
	require.NoError(t, db.Find(&rels).Error)
	require.Equal(t, []types.LegalDocumentRelation{{FromID: "nd68", ToID: "v777", Relation: types.LegalRelationReplaces}},
		[]types.LegalDocumentRelation{{FromID: rels[0].FromID, ToID: rels[0].ToID, Relation: rels[0].Relation}})
	require.Len(t, rels, 1, "the self edge is dropped")

	var got []types.LegalProvisionLink
	require.NoError(t, db.Order("id").Find(&got).Error)
	require.Len(t, got, 2, "the same edge as v777's is merged into it")
	require.Equal(t, "v777", got[0].SourceDocID)
	require.Equal(t, legalcatalog.SyntheticProvisionID("v777", "dieu:2/khoan:1"), got[0].SourceProvisionID)
	require.Equal(t, links[2].ID, got[1].ID)

	var gotC []types.LegalProvisionLinkCandidate
	require.NoError(t, db.Order("id").Find(&gotC).Error)
	require.Equal(t, "v777", gotC[0].AmenderDocID)
	require.Equal(t, legalcatalog.SyntheticProvisionID("v777", "dieu:2/khoan:1"), gotC[0].SourceProvisionID)
	require.JSONEq(t, string(markKey("v777")), string(gotC[0].Detail), "a re-push still recognizes the decided mark")
	require.Equal(t, "v777", gotC[1].TargetDocID)
	require.Equal(t, fpA2, gotC[1].TargetProvisionID)
	require.Equal(t, links[2].ID, *gotC[1].LinkID, "link_id follows the merge")

	var docs []types.LegalDocument
	require.NoError(t, db.Order("id").Find(&docs).Error)
	require.Len(t, docs, 2, "the feed row is gone")
	var v types.LegalDocument
	require.NoError(t, db.First(&v, "id = ?", "v777").Error)
	require.Equal(t, "vbpl", v.Source, "the portal row keeps its source")
	require.Equal(t, "sa", v.ReviewedBy)
	require.Equal(t, "Còn hiệu lực", v.Status, "the feed placeholder status is no curated value: the portal's stays")
	require.Empty(t, v.SourcePending, "vbpl.vn is the standard: its values stay and nothing is parked")

	// only a feed row is adopted
	_, err = AdoptLegalDocument(ctx, db, "nd68", "v777")
	require.Error(t, err)
	_, err = AdoptLegalDocument(ctx, db, "tvpl-777", "v777")
	require.Error(t, err, "already adopted: gone")
}
