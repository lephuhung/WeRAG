package domainpacks

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	legalskillassets "github.com/Tencent/WeKnora/examples/skills"
	"github.com/Tencent/WeKnora/internal/agent/skills"
	"github.com/Tencent/WeKnora/internal/sandbox"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeHostSkill puts a tenant/host skill in dir, standing in for the
// tenant source.
func writeHostSkill(t *testing.T, dir, name, description string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, name), 0o755))
	content := "---\nname: " + name + "\ndescription: " + description + "\n---\nHost instructions for " + name + ".\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, name, "SKILL.md"), []byte(content), 0o644))
}

func newPackManager(t *testing.T, cfg *skills.ManagerConfig, withPacks bool) *skills.Manager {
	t.Helper()
	builtins, err := skills.NewBuiltinSource(legalskillassets.FS)
	require.NoError(t, err)
	mgr := skills.NewManager(cfg, sandbox.NewDisabledManager()).WithBuiltinSource(builtins)
	if withPacks {
		reg, err := Load(os.DirFS("testdata"))
		require.NoError(t, err)
		packs, unknown := reg.Resolve([]string{samplePack})
		require.Empty(t, unknown)
		mgr.WithPackSource(NewSource(packs))
	}
	require.NoError(t, mgr.Initialize(context.Background()))
	return mgr
}

func metadataNames(mgr *skills.Manager) []string {
	var names []string
	for _, meta := range mgr.GetAllMetadata() {
		names = append(names, meta.Name)
	}
	return names
}

func TestManagerShowsPackOnlyWhenWired(t *testing.T) {
	without := newPackManager(t, &skills.ManagerConfig{Enabled: true}, false)
	assert.NotContains(t, metadataNames(without), samplePack)
	assert.False(t, without.IsReadOnlySkill(samplePack))

	with := newPackManager(t, &skills.ManagerConfig{Enabled: true}, true)
	names := metadataNames(with)
	require.Len(t, names, 5)
	assert.Equal(t, samplePack, names[4], "packs come after the built-ins")
	assert.True(t, with.IsReadOnlySkill(samplePack))
	assert.True(t, with.IsBuiltinSkill(samplePack), "the old name stays an alias")
}

func TestManagerPackIgnoresAllowedSkillsAndHidesTenantDuplicate(t *testing.T) {
	dir := t.TempDir()
	writeHostSkill(t, dir, "tenant-only", "A tenant skill")
	writeHostSkill(t, dir, samplePack, "A tenant impostor")
	mgr := newPackManager(t, &skills.ManagerConfig{
		Enabled: true, SkillDirs: []string{dir}, AllowedSkills: []string{"tenant-only", samplePack},
	}, true)

	var packCount int
	for _, meta := range mgr.GetAllMetadata() {
		if meta.Name == samplePack {
			packCount++
			assert.NotEqual(t, "A tenant impostor", meta.Description)
		}
	}
	assert.Equal(t, 1, packCount, "the tenant skill with the pack's name is hidden")
	assert.Contains(t, metadataNames(mgr), "tenant-only")

	skill, err := mgr.LoadSkill(context.Background(), samplePack)
	require.NoError(t, err)
	assert.Contains(t, skill.Instructions, "Gói mẫu")

	// a narrow tenant allowlist does not reach the pack
	narrow := newPackManager(t, &skills.ManagerConfig{
		Enabled: true, SkillDirs: []string{dir}, AllowedSkills: []string{"tenant-only"},
	}, true)
	assert.Contains(t, metadataNames(narrow), samplePack)
	_, err = narrow.LoadSkill(context.Background(), samplePack)
	require.NoError(t, err)
}

func TestManagerReadsOnlyPackResources(t *testing.T) {
	mgr := newPackManager(t, &skills.ManagerConfig{Enabled: true}, true)
	ctx := context.Background()

	content, err := mgr.ReadSkillFile(ctx, samplePack, "references/a.md")
	require.NoError(t, err)
	assert.Contains(t, content, "Ghi chú A")
	files, err := mgr.ListSkillFiles(ctx, samplePack)
	require.NoError(t, err)
	assert.Equal(t, []string{"assets/checklist.md", "references/a.md"}, files)
	for _, hidden := range []string{"catalogue.yaml", "evals/cases.jsonl", "ATTRIBUTION.md"} {
		_, err := mgr.ReadSkillFile(ctx, samplePack, hidden)
		assert.Error(t, err, hidden)
	}

	_, _, err = mgr.PrepareShellEnvironment(ctx, "sess", samplePack, "ls", nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "read-only")
	_, ok := mgr.SandboxSkillDir(samplePack)
	assert.False(t, ok)
}

func TestStripFooterCutsTheToolAnswerBlockToo(t *testing.T) {
	answer := "Trả lời." + types.ToolAnswerBlockMarker + "Giả định: …\n" + FooterMarker + "Lưu ý.\n"
	if got := StripFooter(answer); got != "Trả lời." {
		t.Fatalf("got %q", got)
	}
	if got := StripFooter("Trả lời." + FooterMarker + "x"); got != "Trả lời." {
		t.Fatalf("got %q", got)
	}
	if got := StripFooter("Trả lời."); got != "Trả lời." {
		t.Fatalf("got %q", got)
	}
}
