package service

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Tencent/WeKnora/internal/types"
)

// A copy whose parse failed holds nothing; one still parsing holds the
// document but is not readable; a readable copy comes first.
func TestLegalHeldDocParseStatus(t *testing.T) {
	svc, _ := legalPushTestService(t)
	ctx := context.Background()
	meta, _ := json.Marshal(map[string]string{"external_id": "nd150"})
	set := func(id, parse, enable string) {
		require.NoError(t, svc.db.Model(&types.Knowledge{}).Where("id = ?", id).
			Updates(map[string]any{"parse_status": parse, "enable_status": enable}).Error)
	}

	set("k150", types.ParseStatusFailed, "disabled")
	held, err := svc.heldDoc(ctx, "nd150")
	require.NoError(t, err)
	require.Nil(t, held, "a failed copy is not held")

	set("k150", types.ParseStatusProcessing, "disabled")
	held, err = svc.heldDoc(ctx, "nd150")
	require.NoError(t, err)
	require.NotNil(t, held, "a copy in progress holds the document")
	require.False(t, held.Readable())

	set("k150", types.ParseStatusProcessing, "enabled")
	held, _ = svc.heldDoc(ctx, "nd150")
	require.True(t, held.Readable(), "chunks indexed, summaries still running: readable")

	// a newer readable copy beats an older one still parsing
	set("k150", types.ParseStatusPending, "disabled")
	require.NoError(t, svc.db.Create(&types.Knowledge{ID: "k150b", KnowledgeBaseID: "kb-pub", Title: "150 b",
		ParseStatus: types.ParseStatusCompleted, EnableStatus: "enabled", Metadata: meta}).Error)
	held, _ = svc.heldDoc(ctx, "nd150")
	require.Equal(t, "k150b", held.KnowledgeID)
}

// A push while the amender's copy is parsing marks its candidates as waiting
// (not a false "not found"); the post-parse hook checks them again.
func TestLegalPushWaitsForAParsingCopy(t *testing.T) {
	svc, _ := legalPushTestService(t)
	ctx := context.Background()
	require.NoError(t, svc.db.Model(&types.Knowledge{}).Where("id = ?", "k150").
		Updates(map[string]any{"parse_status": types.ParseStatusProcessing, "enable_status": "disabled"}).Error)

	_, err := svc.Push(ctx, LegalPushInput{Content: []byte(nd68Push), Metadata: legalPushMetaFor("68/2026/NĐ-CP")},
		types.Caller{TenantID: types.PlatformTenantID}, LegalFeedActor{})
	require.NoError(t, err)
	var c types.LegalProvisionLinkCandidate
	require.NoError(t, svc.db.Where("target_path = ? AND amender_doc_id = ?", "dieu:3/khoan:1", "nd150").First(&c).Error)
	require.Equal(t, types.LegalLinkDocOnly, c.MatchStatus)
	require.True(t, strings.Contains(c.MatchReason, legalWaitingForText), c.MatchReason)

	// a knowledge that is no catalogue document's copy: nothing happens
	svc.LegalTextParsed(ctx, "no-such-knowledge")

	// NĐ 150 parsed: its candidate is matched now (a duplicate of the
	// editor's link, closed as in a push)
	require.NoError(t, svc.db.Model(&types.Knowledge{}).Where("id = ?", "k150").
		Updates(map[string]any{"parse_status": types.ParseStatusProcessing, "enable_status": "enabled"}).Error)
	svc.LegalTextParsed(ctx, "k150")
	require.NoError(t, svc.db.First(&c, c.ID).Error)
	require.Equal(t, types.LegalLinkDuplicate, c.MatchStatus, c.MatchReason)
	require.NotContains(t, c.MatchReason, legalWaitingForText)

	// khoản 9 Điều 8 is not in A's copy and khoản 7 not in NĐ 150: still a problem
	var tm types.LegalProvisionLinkCandidate
	require.NoError(t, svc.db.Where("target_path = ?", "dieu:8/khoan:9").First(&tm).Error)
	require.Equal(t, types.LegalLinkTargetMissing, tm.MatchStatus)
}

// The knowledge service runs the legal follow-ups of a parse in order.
func TestLegalAfterParseRunsTheHooksInOrder(t *testing.T) {
	var got []string
	ks := &knowledgeService{}
	ks.AddLegalParsedHook(func(_ context.Context, id string) { got = append(got, "a:"+id) })
	ks.AddLegalParsedHook(func(_ context.Context, id string) { got = append(got, "b:"+id) })
	ks.AddLegalParsedHook(nil)
	ks.legalAfterParse(context.Background(), "k1")
	require.Equal(t, []string{"a:k1", "b:k1"}, got)
}

// The same for the target: A's copy parsing at push time.
func TestLegalPushWaitsForTheTargetsText(t *testing.T) {
	svc, _ := legalPushTestService(t)
	ctx := context.Background()
	require.NoError(t, svc.db.Model(&types.Knowledge{}).Where("id = ?", "k68").
		Updates(map[string]any{"parse_status": types.ParseStatusPending, "enable_status": "disabled"}).Error)
	res, err := svc.Push(ctx, LegalPushInput{Content: []byte(nd68Push), Metadata: legalPushMetaFor("68/2026/NĐ-CP")},
		types.Caller{TenantID: types.PlatformTenantID}, LegalFeedActor{})
	require.NoError(t, err)
	require.Equal(t, LegalPushLinks, res.Mode, "a copy in progress holds A: no second full text")
	var c types.LegalProvisionLinkCandidate
	require.NoError(t, svc.db.Where("target_path = ? AND amender_doc_id = ?", "dieu:3/khoan:2", "nd141").First(&c).Error)
	require.Equal(t, types.LegalLinkTargetMissing, c.MatchStatus)
	require.Contains(t, c.MatchReason, legalWaitingForText)

	require.NoError(t, svc.db.Model(&types.Knowledge{}).Where("id = ?", "k68").
		Updates(map[string]any{"parse_status": types.ParseStatusCompleted, "enable_status": "enabled"}).Error)
	svc.LegalTextParsed(ctx, "k68")
	require.NoError(t, svc.db.First(&c, c.ID).Error)
	require.Equal(t, types.LegalLinkMatched, c.MatchStatus, c.MatchReason)
}
