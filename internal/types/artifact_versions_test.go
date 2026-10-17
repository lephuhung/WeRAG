package types

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestArtifactVersionsPreserveBothFiles(t *testing.T) {
	old := MessageArtifact{URL: "resource://dHZ_fFslfs0GgJGaJZGjGA", FileName: "bao-cao-quy.pptx", SourcePath: "/workspace/output/bao-cao-quy.pptx"}
	next := old
	next.URL = "resource://4N1nAo-FZZoDEExDQz2yoA"
	body := "Đã tạo bản mới. ![PPT](" + old.URL + ")"
	got := ClarifyArtifactVersions(body, MessageArtifacts{next}, MessageArtifacts{old}, "vi-VN")
	require.True(t, strings.HasPrefix(got, body), "must not silently redirect an intentional historical reference")
	require.Contains(t, got, "Phiên bản trước được tham chiếu")
	require.Contains(t, got, "Tệp tạo trong lượt này: ![bao-cao-quy.pptx]("+next.URL+")")
	require.Equal(t, got, ClarifyArtifactVersions(got, MessageArtifacts{next}, MessageArtifacts{old}, "vi-VN"))
	require.Equal(t, body, ClarifyArtifactVersions(body, MessageArtifacts{old}, MessageArtifacts{old}, "vi-VN"))
	// An explicit comparison already contains the new file; leave it alone.
	comparison := body + "\n![bản mới](" + next.URL + ")"
	require.Equal(t, comparison, ClarifyArtifactVersions(comparison, MessageArtifacts{next}, MessageArtifacts{old}, "vi-VN"))
	next.SourcePath = "/workspace/output/other/bao-cao-quy.pptx"
	require.Equal(t, body, ClarifyArtifactVersions(body, MessageArtifacts{next}, MessageArtifacts{old}, "vi-VN"), "matching file names alone cannot establish a version relationship")
}

func TestArtifactVersionsEscapeMarkdownDelimitersInFileName(t *testing.T) {
	old := MessageArtifact{URL: "resource://dHZ_fFslfs0GgJGaJZGjGA", FileName: "báo cáo(bản cuối).pptx", SourcePath: "/workspace/output/báo cáo(bản cuối).pptx"}
	next := old
	next.URL = "resource://4N1nAo-FZZoDEExDQz2yoA"
	got := ClarifyArtifactVersions("![cũ]("+old.URL+")", MessageArtifacts{next}, MessageArtifacts{old}, "vi-VN")
	require.Contains(t, got, "![báo cáo\\(bản cuối\\).pptx]("+old.URL+")")
	require.Contains(t, got, "![báo cáo\\(bản cuối\\).pptx]("+next.URL+")")
	require.True(t, strings.HasSuffix(got, "]("+next.URL+")"))
}
