package repository

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// A parse task loads the row, works, and saves it back: metadata written in
// between (adoption's external_id, the catalogue's eff_status, the retention
// clock) must survive the save; outside such a task a save writes the row
// as given (as before).
func TestUpdateKnowledgeKeepsMetadataWrittenDuringAParse(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&types.Knowledge{}))
	repo := NewKnowledgeRepository(db)
	ctx := context.Background()
	k := &types.Knowledge{TenantID: 1, KnowledgeBaseID: "kb", Type: "file", Title: "nd.html",
		ParseStatus: types.ParseStatusPending, Metadata: types.JSON(`{"external_id":"tvpl-1"}`)}
	require.NoError(t, repo.CreateKnowledge(ctx, k))

	task := types.WithKnowledgeMetadataReadOnly(ctx)
	loaded, err := repo.GetKnowledgeByID(task, 1, k.ID)
	require.NoError(t, err)
	// meanwhile: adoption moves the copy, the import stamps its validity
	require.NoError(t, db.Exec(`UPDATE knowledges SET metadata = ?, custom_metadata = ? WHERE id = ?`,
		`{"external_id":"v1","eff_status":"Còn hiệu lực"}`, `{"note":"x"}`, k.ID).Error)

	loaded.ParseStatus = types.ParseStatusCompleted
	require.NoError(t, repo.UpdateKnowledge(task, loaded))
	got, err := repo.GetKnowledgeByID(ctx, 1, k.ID)
	require.NoError(t, err)
	require.Equal(t, types.ParseStatusCompleted, got.ParseStatus, "the parser's columns are written")
	require.JSONEq(t, `{"external_id":"v1","eff_status":"Còn hiệu lực"}`, string(got.Metadata), "metadata written meanwhile stays")
	require.JSONEq(t, `{"note":"x"}`, string(got.CustomMetadata))

	// an ordinary save (an API edit) still writes what it is given
	got.Metadata = types.JSON(`{"external_id":"v2"}`)
	require.NoError(t, repo.UpdateKnowledge(ctx, got))
	again, _ := repo.GetKnowledgeByID(ctx, 1, k.ID)
	require.JSONEq(t, `{"external_id":"v2"}`, string(again.Metadata))
}
