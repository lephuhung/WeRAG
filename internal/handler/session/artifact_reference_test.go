package session

import (
	"fmt"
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

// handleFor builds a syntactically valid 22-character resource handle for
// artifact i, so tests exercise the same ParseResourcePath path as production.
func handleFor(i int) string {
	base := fmt.Sprintf("art%d", i)
	return base + strings.Repeat("x", types.ResourceHandleLength-len(base))
}

func refFor(i int) string {
	return types.BuildResourcePath(handleFor(i))
}

// artifactsFixture builds artifacts whose storage URL is a catalog handle —
// the normal deployment. Use artifactsWithoutCatalog for the degraded case.
func artifactsFixture(names ...string) types.MessageArtifacts {
	list := make(types.MessageArtifacts, 0, len(names))
	for i, name := range names {
		list = append(list, types.MessageArtifact{FileName: name, URL: refFor(i)})
	}
	return list
}

func artifactsWithoutCatalog(names ...string) types.MessageArtifacts {
	list := make(types.MessageArtifacts, 0, len(names))
	for _, name := range names {
		list = append(list, types.MessageArtifact{FileName: name, URL: "local://7/exports/" + name})
	}
	return list
}

func TestRewriteArtifactReferences(t *testing.T) {
	artifacts := artifactsFixture(
		"Điểm_hồ_sơ_thị_trường_e7edba.html",
		"concept_ranking.csv",
		"trend.png",
		"Vietcombank(VCB) khối lượng giao dịch_838ccc.html",
	)

	cases := []struct {
		name    string
		content string
		want    string
	}{
		{
			name:    "file name with spaces and parentheses",
			content: "![khối lượng](sandbox:Vietcombank(VCB) khối lượng giao dịch_838ccc.html)",
			want:    "![khối lượng](" + refFor(3) + ")",
		},
		{
			name:    "file name with spaces and parentheses, no prefix",
			content: "![khối lượng](Vietcombank(VCB) khối lượng giao dịch_838ccc.html)",
			want:    "![khối lượng](" + refFor(3) + ")",
		},
		{
			name:    "bare file name in image",
			content: "![điểm hồ sơ thị trường](Điểm_hồ_sơ_thị_trường_e7edba.html)",
			want:    "![điểm hồ sơ thị trường](" + refFor(0) + ")",
		},
		{
			name:    "sandbox prefix",
			content: "![điểm](sandbox:Điểm_hồ_sơ_thị_trường_e7edba.html)",
			want:    "![điểm](" + refFor(0) + ")",
		},
		{
			name:    "sandbox scheme with slashes",
			content: "![điểm](sandbox://trend.png)",
			want:    "![điểm](" + refFor(2) + ")",
		},
		{
			name:    "directory prefix is dropped",
			content: "[bảng xếp hạng](/workspace/output/concept_ranking.csv)",
			want:    "[bảng xếp hạng](" + refFor(1) + ")",
		},
		{
			name:    "percent-encoded name",
			content: "![điểm](%C4%90i%E1%BB%83m_h%E1%BB%93_s%C6%A1_th%E1%BB%8B_tr%C6%B0%E1%BB%9Dng_e7edba.html)",
			want:    "![điểm](" + refFor(0) + ")",
		},
		{
			name:    "title is preserved",
			content: `![điểm](trend.png "xu hướng")`,
			want:    `![điểm](` + refFor(2) + ` "xu hướng")`,
		},
		{
			name:    "ordinary link with a colliding name is not rewritten",
			content: "xem [ghi chú](trend.png)",
			want:    "xem [ghi chú](trend.png)",
		},
		{
			name:    "sandbox-prefixed link is rewritten even when not an image",
			content: "số liệu xem [bảng](sandbox:concept_ranking.csv)",
			want:    "số liệu xem [bảng](" + refFor(1) + ")",
		},
		{
			name:    "already-rewritten reference is left alone",
			content: "![điểm](" + refFor(0) + ")",
			want:    "![điểm](" + refFor(0) + ")",
		},
		{
			name:    "prose parentheses are not link destinations",
			content: "Khối lượng giao dịch của Vietcombank(VCB) xem hình dưới.",
			want:    "Khối lượng giao dịch của Vietcombank(VCB) xem hình dưới.",
		},
		{
			name:    "unknown file name untouched",
			content: "![khác](missing.html)",
			want:    "![khác](missing.html)",
		},
		{
			name:    "http url untouched",
			content: "![từ xa](https://example.com/trend.png)",
			want:    "![từ xa](https://example.com/trend.png)",
		},
		{
			name:    "knowledge base image untouched",
			content: "![tài nguyên](resource://abcdefghijklmnopqrstuv)",
			want:    "![tài nguyên](resource://abcdefghijklmnopqrstuv)",
		},
		{
			name:    "fenced code untouched",
			content: "```\n![điểm](trend.png)\n```",
			want:    "```\n![điểm](trend.png)\n```",
		},
		{
			name:    "inline code untouched",
			content: "chỉ cần viết `![điểm](trend.png)`",
			want:    "chỉ cần viết `![điểm](trend.png)`",
		},
		{
			name:    "plain prose untouched",
			content: "Đã tạo hai tệp trend.png và concept_ranking.csv.",
			want:    "Đã tạo hai tệp trend.png và concept_ranking.csv.",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := rewriteArtifactReferences(tc.content, artifacts); got != tc.want {
				t.Fatalf("rewriteArtifactReferences() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestRewriteArtifactReferencesMixedContent(t *testing.T) {
	artifacts := artifactsFixture("chart.html", "data.csv")
	content := "## Biểu đồ\n\n![biểu đồ](chart.html)\n\nsố liệu xem [bảng](sandbox:data.csv)," +
		" liên kết ngoài [tài liệu](https://example.com/chart.html) không bị ảnh hưởng."
	want := "## Biểu đồ\n\n![biểu đồ](" + refFor(0) + ")\n\nsố liệu xem [bảng](" + refFor(1) + ")," +
		" liên kết ngoài [tài liệu](https://example.com/chart.html) không bị ảnh hưởng."

	if got := rewriteArtifactReferences(content, artifacts); got != want {
		t.Fatalf("rewriteArtifactReferences() = %q, want %q", got, want)
	}
}

// A knowledge-base image and a skill artifact routinely appear in the same
// answer. Only the artifact is rebound; the existing reference must survive
// byte for byte, since it already is the canonical form.
func TestRewriteArtifactReferencesKeepsExistingResourceImages(t *testing.T) {
	artifacts := artifactsFixture("chart.html")
	kbImage := types.BuildResourcePath(strings.Repeat("Z", types.ResourceHandleLength))
	content := "![ảnh truy xuất](" + kbImage + ")\n\n![biểu đồ](chart.html)"
	want := "![ảnh truy xuất](" + kbImage + ")\n\n![biểu đồ](" + refFor(0) + ")"

	if got := rewriteArtifactReferences(content, artifacts); got != want {
		t.Fatalf("rewriteArtifactReferences() = %q, want %q", got, want)
	}
}

// Without a resource catalog there is no durable handle, so references are
// normalized to the chat-only sandbox form rather than leaking a storage path.
func TestRewriteArtifactReferencesWithoutCatalog(t *testing.T) {
	artifacts := artifactsWithoutCatalog("chart.html")
	got := rewriteArtifactReferences("![biểu đồ](chart.html)", artifacts)
	if want := "![biểu đồ](sandbox:chart.html)"; got != want {
		t.Fatalf("rewriteArtifactReferences() = %q, want %q", got, want)
	}
	if strings.Contains(got, "local://") {
		t.Fatalf("storage path leaked into content: %q", got)
	}
}

func TestRewriteArtifactReferencesNoArtifacts(t *testing.T) {
	content := "![biểu đồ](chart.html)"
	if got := rewriteArtifactReferences(content, nil); got != content {
		t.Fatalf("rewriteArtifactReferences() = %q, want unchanged", got)
	}
}

func TestArtifactRefByNameKeepsFirstDuplicate(t *testing.T) {
	byName := artifactRefByName(artifactsFixture("a.html", "a.html", "b.html"))
	if byName["a.html"] != refFor(0) {
		t.Fatalf("duplicate name resolved to %q, want %q", byName["a.html"], refFor(0))
	}
	if byName["b.html"] != refFor(2) {
		t.Fatalf("b.html resolved to %q, want %q", byName["b.html"], refFor(2))
	}
}

func TestReferencedArtifactsMatchesNamesAndHandles(t *testing.T) {
	artifacts := artifactsFixture("report.pptx", "chart.html", "data.csv")

	cases := []struct {
		name    string
		content string
		want    types.MessageArtifacts
	}{
		{
			name:    "sandbox-prefixed name",
			content: "Đã tạo ![báo cáo](sandbox:report.pptx)",
			want:    types.MessageArtifacts{artifacts[0]},
		},
		{
			name:    "bare name in an image",
			content: "![biểu đồ](chart.html)",
			want:    types.MessageArtifacts{artifacts[1]},
		},
		{
			name:    "output path in an ordinary link",
			content: "[số liệu](./output/data.csv)",
			want:    types.MessageArtifacts{artifacts[2]},
		},
		{
			name:    "canonical handle",
			content: "![báo cáo](" + refFor(0) + ")",
			want:    types.MessageArtifacts{artifacts[0]},
		},
		{
			name:    "prose mention is not a reference",
			content: "Đã tạo hai tệp report.pptx và chart.html.",
			want:    nil,
		},
		{
			name:    "bare name in an ordinary link is not a reference",
			content: "xem [ghi chú](report.pptx)",
			want:    nil,
		},
		{
			name:    "unknown name",
			content: "![khác](missing.pptx)",
			want:    nil,
		},
		{
			name:    "code sample is not a reference",
			content: "```\n![báo cáo](sandbox:report.pptx)\n```",
			want:    nil,
		},
		{
			// Go regexp.Split does not interleave the matched fences, so
			// walking parts with i+=2 would skip the segment after the first
			// fence and miss a real citation that rewrite still rewrites.
			name:    "reference after a code fence",
			content: "![báo cáo](sandbox:report.pptx)\n\n```\n![bỏ qua](sandbox:chart.html)\n```\n\n![biểu đồ](sandbox:chart.html)",
			want:    types.MessageArtifacts{artifacts[0], artifacts[1]},
		},
		{
			name:    "multiple references keep candidate order",
			content: "![số liệu](data.csv)\n\n![báo cáo](sandbox:report.pptx)",
			want:    types.MessageArtifacts{artifacts[0], artifacts[2]},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, referencedArtifacts(tc.content, artifacts))
		})
	}
}

// A regenerated file shadows the older version of itself: the name resolves to
// the first candidate, while an explicit handle still reaches the old one.
func TestReferencedArtifactsPrefersTheFirstCandidate(t *testing.T) {
	old := types.MessageArtifact{FileName: "deck.pptx", URL: refFor(0)}
	fresh := types.MessageArtifact{FileName: "deck.pptx", URL: refFor(1)}
	candidates := types.MessageArtifacts{fresh, old}

	require.Equal(t, types.MessageArtifacts{fresh},
		referencedArtifacts("![deck](sandbox:deck.pptx)", candidates))
	require.Equal(t, types.MessageArtifacts{old},
		referencedArtifacts("![deck]("+old.URL+")", candidates))
}

// KnownArtifacts is oldest-first. Reverse it before merging so a hash-skipped
// name binds the latest file; this turn still shadows that latest file.
func TestMergeArtifactListsPrefersThisTurnThenLatestKnown(t *testing.T) {
	old := types.MessageArtifact{FileName: "deck.pptx", URL: refFor(0)}
	latest := types.MessageArtifact{FileName: "deck.pptx", URL: refFor(1)}
	fresh := types.MessageArtifact{FileName: "deck.pptx", URL: refFor(2)}
	known := types.MessageArtifacts{old, latest}

	require.Equal(t, types.MessageArtifacts{latest},
		referencedArtifacts("![deck](sandbox:deck.pptx)",
			mergeArtifactLists(nil, artifactsNewestFirst(known))))
	require.Equal(t, types.MessageArtifacts{fresh},
		referencedArtifacts("![deck](sandbox:deck.pptx)",
			mergeArtifactLists(types.MessageArtifacts{fresh}, artifactsNewestFirst(known))))
}

func TestArtifactsNewestFirstDoesNotMutateInput(t *testing.T) {
	in := artifactsFixture("a.html", "b.html")
	got := artifactsNewestFirst(in)
	require.Equal(t, "b.html", got[0].FileName)
	require.Equal(t, "a.html", got[1].FileName)
	require.Equal(t, "a.html", in[0].FileName)
}
