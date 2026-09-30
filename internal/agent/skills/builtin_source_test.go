package skills

import (
	"errors"
	"io/fs"
	"slices"
	"strings"
	"testing"
	"testing/fstest"

	legalskillassets "github.com/Tencent/WeKnora/examples/skills"
	"github.com/stretchr/testify/require"
)

var expectedBuiltinNames = []string{
	"legal-document-summary",
	"legal-document-comparison",
	"legal-latest-guidance",
	"legal-question-abbreviations",
}

func TestBuiltinSourceEmbedsOnlyFourReadableLegalSkills(t *testing.T) {
	src, err := NewBuiltinSource(legalskillassets.FS)
	require.NoError(t, err)
	metadata, err := src.DiscoverSkills()
	require.NoError(t, err)
	var names []string
	for _, meta := range metadata {
		names = append(names, meta.Name)
		require.Equal(t, "skill://"+meta.Name, meta.BasePath)
		skill, err := src.LoadSkillInstructions(meta.Name)
		require.NoError(t, err)
		require.NotEmpty(t, strings.TrimSpace(skill.Instructions))
		require.Equal(t, "skill://"+meta.Name+"/SKILL.md", skill.FilePath)
		file, err := src.LoadSkillFile(meta.Name, "SKILL.md")
		require.NoError(t, err)
		require.Contains(t, file.Content, "name: "+meta.Name)
		files, err := src.ListSkillFiles(meta.Name)
		require.NoError(t, err)
		require.Empty(t, files, "built-in instructions ship without executable resources")
	}
	require.Equal(t, expectedBuiltinNames, names)
	require.NotContains(t, names, "pdf-processing")
}

func TestBuiltinSourceRejectsUnexpectedResourcesAndUnknownNames(t *testing.T) {
	src, err := NewBuiltinSource(legalskillassets.FS)
	require.NoError(t, err)
	for _, name := range []string{"legal-document-summary", "pdf-processing", "../legal-document-summary"} {
		_, err := src.LoadSkillFile(name, "scripts/run.sh")
		require.Error(t, err)
	}
	_, err = src.LoadSkillInstructions("pdf-processing")
	require.Error(t, err)
	_, err = src.GetSkillBasePath("pdf-processing")
	require.Error(t, err)
	require.True(t, src.Has("legal-document-summary"))
	require.False(t, src.Has("pdf-processing"))
}

func TestBuiltinSourceRejectsBrokenBundledInstructions(t *testing.T) {
	cases := []struct {
		name   string
		change func(fstest.MapFS)
	}{
		{"missing", func(files fstest.MapFS) { delete(files, "legal-document-summary/SKILL.md") }},
		{"wrong-name", func(files fstest.MapFS) {
			files["legal-document-summary/SKILL.md"] = &fstest.MapFile{Data: []byte("---\nname: wrong\ndescription: x\n---\nbody")}
		}},
		{"empty-body", func(files fstest.MapFS) {
			files["legal-document-summary/SKILL.md"] = &fstest.MapFile{Data: []byte("---\nname: legal-document-summary\ndescription: x\n---\n   \n")}
		}},
		{"broken-frontmatter", func(files fstest.MapFS) {
			files["legal-document-summary/SKILL.md"] = &fstest.MapFile{Data: []byte("name: legal-document-summary\ndescription: x\nbody")}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			files := fstest.MapFS{}
			for _, name := range expectedBuiltinNames {
				path := name + "/SKILL.md"
				data, err := fs.ReadFile(legalskillassets.FS, path)
				require.NoError(t, err)
				files[path] = &fstest.MapFile{Data: data}
			}
			tc.change(files)
			_, err := NewBuiltinSource(files)
			require.True(t, errors.Is(err, ErrInvalidBuiltin), "error should name broken built-in: %v", err)
		})
	}
}

func TestBuiltinSourceNameListCannotBeChangedByExtraAssets(t *testing.T) {
	files := fstest.MapFS{}
	for _, name := range expectedBuiltinNames {
		path := name + "/SKILL.md"
		data, err := fs.ReadFile(legalskillassets.FS, path)
		require.NoError(t, err)
		files[path] = &fstest.MapFile{Data: data}
	}
	files["pdf-processing/SKILL.md"] = &fstest.MapFile{Data: []byte("---\nname: pdf-processing\ndescription: not built in\n---\nbody")}
	src, err := NewBuiltinSource(files)
	require.NoError(t, err)
	got, err := src.DiscoverSkills()
	require.NoError(t, err)
	require.Len(t, got, 4)
	require.True(t, slices.Contains(expectedBuiltinNames, got[0].Name))
	require.False(t, src.Has("pdf-processing"))
}
