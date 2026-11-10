package repository

import (
	"context"
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/google/uuid"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestLegalCatalogNamedCopiesAndArticleChunks(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&types.LegalDocument{}); err != nil {
		t.Fatal(err)
	}
	for _, stmt := range []string{
		`CREATE TABLE knowledges (id TEXT PRIMARY KEY, knowledge_base_id TEXT, metadata TEXT, parse_status TEXT, deleted_at DATETIME, created_at DATETIME)`,
		`CREATE TABLE chunks (id TEXT PRIMARY KEY, knowledge_id TEXT, chunk_index INTEGER, chunk_type TEXT, is_enabled BOOLEAN, metadata TEXT, deleted_at DATETIME, index_status TEXT)`,
		`INSERT INTO knowledges VALUES
			('k15', 'kb-1', '{"external_id":"d15"}', 'completed', NULL, '2026-01-01'),
			('k15-other', 'kb-9', '{"external_id":"d15"}', 'completed', NULL, '2026-01-01'),
			('k15-parsing', 'kb-1', '{"external_id":"d15"}', 'pending', NULL, '2026-01-02')`,
		`INSERT INTO chunks VALUES
			('c1sync', 'k15', 0, 'text', 1, '{"legal":{"article_nos":["1"]}}', NULL, 'processing'),
			('c1', 'k15', 1, 'text', 1, '{"legal":{"article_nos":["1"]}}', NULL, 'indexed'),
			('c10a', 'k15', 10, 'text', 1, '{"legal":{"article_nos":["10"]}}', NULL, NULL),
			('c10b', 'k15', 11, 'text', 1, '{"legal":{"article_nos":["10"]}}', NULL, NULL),
			('c1off', 'k15', 0, 'text', 0, '{"legal":{"article_nos":["1"]}}', NULL, NULL),
			('c100', 'k15', 12, 'text', 1, '{"legal":{"article_nos":["100"]}}', NULL, NULL)`,
	} {
		if err := db.Exec(stmt).Error; err != nil {
			t.Fatal(err)
		}
	}
	repo := &legalCatalogRepository{db: db}
	ctx := context.Background()
	if err := repo.UpsertDocuments(ctx, []*types.LegalDocument{
		{ID: "d15", Source: "tvpl", DocumentNumber: "15/2026/NĐ-CP", NumberKey: "15/2026/ND-CP"},
		{ID: "d15-other", Source: "vbpl", DocumentNumber: "15/2026/NĐ-CP", NumberKey: "15/2026/ND-CP"},
	}); err != nil {
		t.Fatal(err)
	}
	copies, err := repo.NamedCopies(ctx, []string{"15/2026/ND-CP"}, []string{"kb-1"}, 10)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, c := range copies {
		got = append(got, c.Document.ID+"="+c.KnowledgeID)
	}
	if strings.Join(got, ",") != "d15=k15,d15-other=" {
		t.Fatalf("copies = %v, want the parsed copy in scope and the other document without one", got)
	}
	chunks, err := repo.ArticleChunkIDs(ctx, []string{"k15"}, []string{"1", "10"}, 1)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(chunks["k15\x001"], ",") != "c1" || strings.Join(chunks["k15\x0010"], ",") != "c10a" {
		t.Fatalf("chunks = %v", chunks)
	}
}
