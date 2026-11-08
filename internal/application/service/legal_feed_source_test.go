package service

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
	"github.com/Tencent/WeKnora/internal/vbpl"
	"github.com/Tencent/WeKnora/internal/vietnamese_legal"
)

func TestLegalFeedSource(t *testing.T) {
	know := func(m map[string]string) *types.Knowledge {
		b, _ := json.Marshal(m)
		return &types.Knowledge{Metadata: b}
	}
	feed := know(map[string]string{"source": "tvpl", "format": vbpl.LegalHTMLFormatV1})
	require.Equal(t, "tvpl", legalFeedSource(feed))
	require.True(t, vbplSkipsQuestionGeneration(feed), "a feed's legal document skips generated questions like vbpl's")
	portal := know(map[string]string{"source": "vbpl"})
	require.Empty(t, legalFeedSource(portal))
	require.True(t, vbplSkipsQuestionGeneration(portal))
	upload := know(map[string]string{"source": "tvpl"}) // no legal HTML format
	require.Empty(t, legalFeedSource(upload))
	require.False(t, vbplSkipsQuestionGeneration(upload))
	require.Empty(t, legalFeedSource(&types.Knowledge{}))
	require.Empty(t, legalFeedSource(nil))
}

// A made-up (fp-) target id is no chunk's id: the provision is matched by
// its number, even in a chunk that records portal ids.
func TestLegalEditorTargetSyntheticID(t *testing.T) {
	fp := legalcatalog.SyntheticProvisionID("nd68", "dieu:3/khoan:1")
	ref := legalEditorTarget{}.editorTarget(types.LegalProvisionLink{TargetProvisionID: fp}, LegalProvisionRef{Article: "3", Khoan: "1"})
	require.Empty(t, ref.ID)
	require.Equal(t, "khoan:1", ref.Key)

	chunk := LegalProvisionChunk{Legal: &vietnamese_legal.LegalMetadata{ArticleNos: []string{"3"},
		ProvisionIDs: map[string]string{"dieu": "p68-3", "khoan:1": "p68-3-1"}}}
	e := LegalAmendEdge{EditorTargets: []LegalEditorTargetRef{ref}}
	holds, ambiguous := e.editorHolds(chunk)
	require.True(t, holds)
	require.False(t, ambiguous)

	e.EditorTargets[0].ID = "p-other"
	holds, _ = e.editorHolds(chunk)
	require.False(t, holds, "a portal id still tells another provision apart")
}

// B held as a feed's text (no provision ids in its chunks, links naming
// B's provisions by synthetic ids): B's wording is found by source_path.
func TestLegalEditorSourceTextByPath(t *testing.T) {
	db := legalEditorTestDB(t)
	ctx := context.Background()
	km, _ := json.Marshal(map[string]string{"doc_number": "150/2026/NĐ-CP", "external_id": "nd150", "doc_type": "Nghị định",
		"source": "tvpl", "format": vbpl.LegalHTMLFormatV1})
	require.NoError(t, db.Model(&types.Knowledge{}).Where("id = ?", "k150").Update("metadata", km).Error)
	var c types.Chunk
	require.NoError(t, db.First(&c, "id = ?", "b150").Error)
	var meta types.DocumentChunkMetadata
	require.NoError(t, json.Unmarshal(c.Metadata, &meta))
	meta.Legal.Source, meta.Legal.ProvisionIDs = "tvpl", nil
	cm, _ := json.Marshal(meta)
	require.NoError(t, db.Model(&types.Chunk{}).Where("id = ?", "b150").Update("metadata", cm).Error)
	var links []types.LegalProvisionLink
	require.NoError(t, db.Where("source_doc_id = ?", "nd150").Find(&links).Error)
	for _, lk := range links {
		require.NoError(t, db.Model(&types.LegalProvisionLink{}).Where("id = ?", lk.ID).
			Update("source_provision_id", legalcatalog.SyntheticProvisionID("nd150", lk.SourcePath)).Error)
	}

	load, err := LoadLegalAmendmentsWith(ctx, db, []string{"kb-pub"}, []string{"a3", "a8"}, []string{"kb-pub"}, time.UTC,
		LegalAmendLoadOptions{EditorSource: true})
	require.NoError(t, err)
	require.Equal(t, 3, load.Editor.WithText, "the same texts as with portal ids")
	q := editorEdgeOf(t, load, LegalOpSuaDoi, LegalProvisionRef{Article: "3", Khoan: "1"}, "nd150")
	require.Equal(t, "1. Khoản một mới theo Nghị định 150.", q.QuoteText)
	require.Equal(t, "b150", q.Amender.ChunkID)
}

func TestLegalFeedAdoptFeedTwin(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&types.LegalDocument{}, &types.LegalDocumentRelation{}, &types.LegalDocumentLink{},
		&types.LegalDocumentRequest{}, &types.LegalDocumentDraft{}, &types.LegalProvisionLink{}, &types.LegalProvisionLinkCandidate{},
		&types.KnowledgeBase{}, &types.Knowledge{}, &types.Chunk{}))
	ctx := context.Background()
	day := func(s string) *time.Time { d, _ := time.Parse("2006-01-02", s); return &d }
	key := vietnamese_legal.CatalogNumberKey("777/2026/NĐ-CP")
	doc := func(id, source, title string) types.LegalDocument {
		return types.LegalDocument{ID: id, Source: source, DocumentNumber: "777/2026/NĐ-CP", NumberKey: key, DocType: "Nghị định",
			Issuer: "Chính phủ", IssuedOn: day("2026-11-05"), Title: title}
	}
	require.NoError(t, db.Create(&[]types.LegalDocument{
		doc("tvpl-777", "tvpl", "Nghị định 777/2026/NĐ-CP quy định chi tiết về thuế hộ kinh doanh"),
		doc("v777", "vbpl", "Nghị định số 777/2026/NĐ-CP Quy định chi tiết về thuế hộ kinh doanh"),
	}).Error)
	require.NoError(t, db.Create(&types.KnowledgeBase{ID: "kb-pub", Name: LegalKBNamePrefix + "Nghị định"}).Error)
	km, _ := json.Marshal(map[string]string{"external_id": "tvpl-777", "source": "tvpl"})
	require.NoError(t, db.Create(&types.Knowledge{ID: "k1", KnowledgeBaseID: "kb-pub", Metadata: km}).Error)
	feed := &LegalFeedService{db: db}

	// the push of the feed row itself passes through
	require.NoError(t, feed.adoptFeedTwin(ctx, "tvpl-777", "777/2026/NĐ-CP", "kb-pub"))
	held, err := feed.heldPublic(ctx, "tvpl-777")
	require.NoError(t, err)
	require.Equal(t, []string{"kb-pub"}, held)

	// a portal id without its catalogue row while a feed copy is held: refused
	err = feed.adoptFeedTwin(ctx, "v-unknown", "777/2026/NĐ-CP", "kb-pub")
	var refusal *LegalFeedRefusal
	require.ErrorAs(t, err, &refusal)

	// the feed copy sits in another KB than the ingest's: refused, nothing moves
	err = feed.adoptFeedTwin(ctx, "v777", "777/2026/NĐ-CP", "kb-other")
	require.ErrorAs(t, err, &refusal)
	held, err = feed.heldPublic(ctx, "tvpl-777")
	require.NoError(t, err)
	require.Equal(t, []string{"kb-pub"}, held, "the feed row was not adopted")

	// the portal row: the feed row hands its KB copy over before the ingest
	require.NoError(t, feed.adoptFeedTwin(ctx, "v777", "777/2026/NĐ-CP", "kb-pub"))
	held, err = feed.heldPublic(ctx, "v777")
	require.NoError(t, err)
	require.Equal(t, []string{"kb-pub"}, held, "the KB copy now answers to the portal id: the ingest replaces it in place")
	var n int64
	require.NoError(t, db.Model(&types.LegalDocument{}).Where("id = ?", "tvpl-777").Count(&n).Error)
	require.Zero(t, n)

	// two feed rows of the same document: a SuperAdmin decides
	require.NoError(t, db.Create(&[]types.LegalDocument{
		doc("tvpl-a", "tvpl", "Nghị định 777/2026/NĐ-CP quy định chi tiết về thuế hộ kinh doanh"),
		doc("tvpl-b", "tvpl", "Nghị định 777/2026/NĐ-CP quy định chi tiết về thuế hộ kinh doanh"),
	}).Error)
	err = feed.adoptFeedTwin(ctx, "v777", "777/2026/NĐ-CP", "kb-pub")
	require.ErrorAs(t, err, &refusal)
}
