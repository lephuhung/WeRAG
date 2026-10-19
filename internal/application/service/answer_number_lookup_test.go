package service

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/vietnamese_legal"
)

func TestCatalogNumberLookupIsExact(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&types.LegalDocument{}))
	repo := repository.NewLegalCatalogRepository(db)
	ctx := context.Background()
	require.NoError(t, repo.UpsertDocuments(ctx, []*types.LegalDocument{
		{ID: "a", DocumentNumber: "168/2025/NĐ-CP", NumberKey: vietnamese_legal.CatalogNumberKey("168/2025/NĐ-CP")},
		{ID: "b", DocumentNumber: "30/2020/NĐ-CP", NumberKey: "30/2020/ND-CP"},
	}))
	lookup := catalogNumberLookup(repo)
	for key, want := range map[string]bool{
		vietnamese_legal.CatalogNumberKey("168/2025/NĐ–CP"): true, // dash variant, same key
		"30/2020": false, // a prefix match is not the number
		"132/2023/ND-CP": false,
	} {
		got, err := lookup(ctx, key)
		require.NoError(t, err)
		assert.Equal(t, want, got, key)
	}
}

func TestDocumentNumberCheckDefault(t *testing.T) {
	on, off := true, false
	assert.True(t, types.CustomAgentConfig{}.DocumentNumberCheck(true), "packs: on by default")
	assert.False(t, types.CustomAgentConfig{}.DocumentNumberCheck(false), "no packs: off by default")
	assert.True(t, types.CustomAgentConfig{VerifyDocumentNumbers: &on}.DocumentNumberCheck(false))
	assert.False(t, types.CustomAgentConfig{VerifyDocumentNumbers: &off}.DocumentNumberCheck(true))
}
