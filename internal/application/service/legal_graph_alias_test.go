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

func TestResolveLegalDocumentAliases(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&types.LegalDocument{}))
	ctx := context.Background()
	catalog := repository.NewLegalCatalogRepository(db)
	day := func(s string) *time.Time { d, _ := time.Parse("2006-01-02", s); return &d }
	require.NoError(t, catalog.UpsertDocuments(ctx, []*types.LegalDocument{
		{ID: "a", DocType: "Luật", DocumentNumber: "29/2018/QH14", Title: "Luật Bảo vệ bí mật nhà nước số 29/2018/QH14", IssuedOn: day("2018-11-15")},
		{ID: "b", DocType: "Luật", DocumentNumber: "117/2025/QH15", Title: "Luật Bảo vệ bí mật nhà nước số 117/2025/QH15", IssuedOn: day("2025-12-10")},
		{ID: "c", DocType: "Hiến pháp", DocumentNumber: "Không số", Title: "Hiến pháp năm 2013", IssuedOn: day("2013-11-28")},
		{ID: "d", DocType: "Luật", DocumentNumber: "24/2018/QH14", Title: "An ninh mạng", IssuedOn: day("2018-06-12")},
	}))

	for title, want := range map[string]string{
		"Luật Bảo Vệ Bí Mật Nhà Nước 2018":     "29/2018/QH14",
		"Luật Bảo vệ bí mật nhà nước năm 2025": "117/2025/QH15",
		"Luật Bảo Vệ Bí Mật Nhà Nước":          "", // two versions, no year
		"Luật Bí Mật Nhà Nước 2018":            "", // not the title
		"Hiến Pháp Năm 2013":                   "", // no số hiệu to key by
		"Luật An Ninh Mạng 2018":               "24/2018/QH14",
		"Quyết Định Giải Mật":                  "",
	} {
		require.Equal(t, want, catalogNumberForTitle(ctx, catalog, title), title)
	}

	g := &types.GraphData{
		Node: []*types.GraphNode{
			{Name: "29/2018/QH14", Type: "Document", Attributes: []string{"root"}},
			{Name: "Luật Bảo Vệ Bí Mật Nhà Nước 2018", Type: "Document", Attributes: []string{"alias"}},
			{Name: "Điều 5 (24/2018/QH14)", Type: "Article"},
		},
		Relation: []*types.GraphRelation{
			{Node1: "Luật Bảo Vệ Bí Mật Nhà Nước 2018", Type: "VIEN_DAN", Node2: "Luật An Ninh Mạng 2018"},
			{Node1: "29/2018/QH14", Type: "SUA_DOI", Node2: "Luật Bảo Vệ Bí Mật Nhà Nước 2018"},
		},
	}
	g = resolveLegalDocumentAliases(ctx, catalog, g)
	require.Len(t, g.Node, 2, "the alias merges into the số hiệu node")
	require.Equal(t, []string{"root", "alias"}, g.Node[0].Attributes)
	require.Len(t, g.Relation, 1, "an edge that became a self-loop is dropped")
	require.Equal(t, "29/2018/QH14", g.Relation[0].Node1)
}
