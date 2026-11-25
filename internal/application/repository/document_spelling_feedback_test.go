package repository

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/Tencent/WeKnora/internal/types"
)

// Only the user's "not a mistake" suppresses an underline: accepted/fixed
// verdicts (the user changed the text) and verifier rows do not.
func TestDocumentSpellingFeedbackDismissedOnlyNotError(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&types.DocumentSpellingFeedback{}))
	repo := NewDocumentSpellingFeedbackRepository(db)
	ctx := context.Background()
	row := func(wrong, verdict, source, correction string) types.DocumentSpellingFeedback {
		return types.DocumentSpellingFeedback{
			TenantID: 1, ParagraphHash: "h1", Paragraph: "p", Wrong: wrong,
			Verdict: verdict, Source: source, Correction: correction,
		}
	}
	require.NoError(t, repo.Record(ctx, []types.DocumentSpellingFeedback{
		row("đúng", types.SpellingVerdictNotError, types.SpellingFeedbackSourceUser, ""),
		row("sai", types.SpellingVerdictAccepted, types.SpellingFeedbackSourceUser, ""),
		row("lỗi", types.SpellingVerdictFixed, types.SpellingFeedbackSourceUser, "lối"),
		row("mô hình", types.SpellingVerdictNotError, types.SpellingFeedbackSourceVerifier, ""),
	}))
	got, err := repo.Dismissed(ctx, 1, []string{"h1"})
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, "đúng", got[0].Wrong)

	var fixed types.DocumentSpellingFeedback
	require.NoError(t, db.Where("verdict = ?", types.SpellingVerdictFixed).First(&fixed).Error)
	require.Equal(t, "lối", fixed.Correction)
	require.Equal(t, "lối", types.SpellingLabel(&fixed).UserCorrection)
}
