package repository

import (
	"context"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestMessageDocumentSelectionRoundTrip(t *testing.T) {
	repo, db := newMessageRepositoryForForkTest(t)
	ctx := context.Background()
	at := time.Date(2026, 10, 7, 9, 0, 0, 0, time.UTC)

	require.NoError(t, db.Session(&gorm.Session{SkipHooks: true}).Create(&types.Message{
		ID: "u-sel", SessionID: "s1", RequestID: "r1", Role: "user", CreatedAt: at,
		DocumentSelection: types.NewMessageDocumentSelection(&types.DocumentSelection{
			Text: "Điều 3. Hiệu lực thi hành", ParagraphHint: "Điều 3. Hiệu lực thi hành kể từ ngày ký",
		}),
	}).Error)
	require.NoError(t, db.Session(&gorm.Session{SkipHooks: true}).Create(&types.Message{
		ID: "u-plain", SessionID: "s1", RequestID: "r2", Role: "user", CreatedAt: at.Add(time.Second),
	}).Error)

	got, err := repo.GetMessage(ctx, "s1", "u-sel")
	require.NoError(t, err)
	require.NotNil(t, got.DocumentSelection)
	require.Equal(t, "Điều 3. Hiệu lực thi hành", got.DocumentSelection.Text)
	require.Equal(t, "Điều 3. Hiệu lực thi hành kể từ ngày ký", got.DocumentSelection.ParagraphHint)

	plain, err := repo.GetMessage(ctx, "s1", "u-plain")
	require.NoError(t, err)
	require.Nil(t, plain.DocumentSelection)

	list, err := repo.GetMessagesBySession(ctx, "s1", 1, 10)
	require.NoError(t, err)
	require.Len(t, list, 2)
	require.NotNil(t, list[0].DocumentSelection)
	require.Equal(t, "Điều 3. Hiệu lực thi hành", list[0].DocumentSelection.Text)
	require.Nil(t, list[1].DocumentSelection)
}
