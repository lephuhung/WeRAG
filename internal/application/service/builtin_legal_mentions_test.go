package service

import (
	"context"
	"testing"

	legalskillassets "github.com/Tencent/WeKnora/examples/skills"
	"github.com/Tencent/WeKnora/internal/agent/skills"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func TestBuiltinLegalMentionPinsOnlyAvailableBuiltinsInNoneMode(t *testing.T) {
	cfg := &types.AgentConfig{SkillsEnabled: false, AllowedSkills: []string{"tenant-only"}}
	applyPerRequestSkillScope(context.Background(), cfg, "none", []string{
		"unknown", "legal-document-summary", "tenant-only", "legal-document-summary", "legal-question-abbreviations",
	})
	require.Equal(t, []string{"legal-document-summary", "legal-question-abbreviations"}, cfg.PinnedSkillNames)
	require.False(t, cfg.SkillsEnabled)
	require.Equal(t, []string{"tenant-only"}, cfg.AllowedSkills)
}

func TestBuiltinLegalMentionUsesAvailableDescriptionOverTenantCollision(t *testing.T) {
	builtins, err := skills.NewBuiltinSource(legalskillassets.FS)
	require.NoError(t, err)
	mgr := skills.NewManager(&skills.ManagerConfig{Enabled: true}, nil).WithBuiltinSource(builtins)
	require.NoError(t, mgr.Initialize(context.Background()))
	cfg := &types.AgentConfig{
		PinnedSkillNames: []string{"unknown", "legal-document-summary"},
		TenantSkills:     []*types.TenantSkillEntity{{Name: "legal-document-summary", Description: "UNTRUSTED DUPLICATE"}},
	}
	pinned := (&agentService{}).resolvePinnedSkillInfos(cfg, mgr)
	require.Len(t, pinned, 1)
	require.Equal(t, "legal-document-summary", pinned[0].Name)
	require.NotEmpty(t, pinned[0].Description)
	require.NotEqual(t, "UNTRUSTED DUPLICATE", pinned[0].Description)
}
