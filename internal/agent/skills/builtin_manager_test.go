package skills

import (
	"context"
	"testing"

	legalskillassets "github.com/Tencent/WeKnora/examples/skills"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func TestManagerBuiltinSkillsBypassTenantAllowlistButRemainReadOnly(t *testing.T) {
	ctx := context.Background()
	builtins, err := NewBuiltinSource(legalskillassets.FS)
	require.NoError(t, err)
	mgr := NewManager(&ManagerConfig{Enabled: true, AllowedSkills: []string{"tenant-only"}}, nil).WithBuiltinSource(builtins)
	require.NoError(t, mgr.Initialize(ctx))
	metadata := mgr.GetAllMetadata()
	require.Len(t, metadata, 4)
	for i, meta := range metadata {
		require.Equal(t, expectedBuiltinNames[i], meta.Name)
		skill, err := mgr.LoadSkill(ctx, meta.Name)
		require.NoError(t, err)
		require.NotEmpty(t, skill.Instructions)
		content, err := mgr.ReadSkillFile(ctx, meta.Name, SkillFileName)
		require.NoError(t, err)
		require.Contains(t, content, "name: "+meta.Name)
		require.True(t, mgr.IsBuiltinSkill(meta.Name))
		_, available := mgr.SandboxSkillDir(meta.Name)
		require.False(t, available)
		_, _, err = mgr.PrepareShellEnvironment(ctx, "session", meta.Name, "true", nil)
		require.ErrorContains(t, err, "read-only")
	}
	_, err = mgr.LoadSkill(ctx, "not-listed")
	require.Error(t, err)
}

func TestManagerBuiltinNamesOverrideTenantDuplicatesWithoutHostFallback(t *testing.T) {
	ctx := context.Background()
	builtins, err := NewBuiltinSource(legalskillassets.FS)
	require.NoError(t, err)
	rows := []*types.TenantSkillEntity{
		{Name: "legal-document-summary", Description: "UNTRUSTED DUPLICATE", Instructions: "UNTRUSTED BODY", Enabled: true, Status: types.SkillStatusReady},
		{Name: "tenant-only", Description: "tenant helper", Instructions: "tenant body", Enabled: true, Status: types.SkillStatusReady},
		{Name: "other", Description: "should not appear", Enabled: true, Status: types.SkillStatusReady},
	}
	mgr := NewManager(&ManagerConfig{Enabled: true, AllowedSkills: []string{"tenant-only"}}, nil).
		WithTenantSource(NewTenantSkillSource(rows, nil)).WithBuiltinSource(builtins)
	for _, action := range []string{"Initialize", "Reload"} {
		if action == "Initialize" {
			require.NoError(t, mgr.Initialize(ctx))
		} else {
			require.NoError(t, mgr.Reload(ctx))
		}
		var names []string
		for _, meta := range mgr.GetAllMetadata() {
			names = append(names, meta.Name)
			require.NotEqual(t, "UNTRUSTED DUPLICATE", meta.Description)
		}
		require.Equal(t, append(append([]string{}, expectedBuiltinNames...), "tenant-only"), names)
		summary, err := mgr.LoadSkill(ctx, "legal-document-summary")
		require.NoError(t, err)
		require.NotContains(t, summary.Instructions, "UNTRUSTED BODY")
		tenant, err := mgr.LoadSkill(ctx, "tenant-only")
		require.NoError(t, err)
		require.Equal(t, "tenant body", tenant.Instructions)
		_, err = mgr.LoadSkill(ctx, "other")
		require.Error(t, err)
		_, _, err = mgr.PrepareShellEnvironment(ctx, "session", "legal-document-summary", "true", nil)
		require.ErrorContains(t, err, "read-only")
	}
}
