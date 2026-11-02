package agent

import (
	"context"
	"encoding/xml"
	"io"
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/agent/tools"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func TestFormatKnowledgeBaseListRendersGeneratedProfile(t *testing.T) {
	kbs := []*KnowledgeBaseInfo{{
		ID: "kb-1", Name: "Ops", Type: "document", Description: "Manual description",
		Profile: &types.KnowledgeBaseProfile{
			Gist:             "Runbooks & guides for the <cluster>",
			Topics:           []string{"Kubernetes", "Ingress"},
			TypicalQuestions: []string{"How do I roll back a deploy?", "q2", "q3", "q4", "q5", "q6"},
		},
	}}
	text := formatKnowledgeBaseList(kbs)
	require.Contains(t, text, "<description>Manual description</description>")
	require.Contains(t, text, "<generated_profile>")
	require.Contains(t, text, "<gist>Runbooks &amp; guides for the &lt;cluster&gt;</gist>")
	require.Contains(t, text, "<topics>Kubernetes, Ingress</topics>")
	require.Contains(t, text, "<question>How do I roll back a deploy?</question>")
	require.NotContains(t, text, "<question>q6</question>", "questions are capped")
	decoder := xml.NewDecoder(strings.NewReader(text))
	for {
		_, err := decoder.Token()
		if err == io.EOF {
			break
		}
		require.NoError(t, err, "untrusted profile text must be escaped")
	}
}

func TestFormatKnowledgeBaseListOmitsEmptyProfile(t *testing.T) {
	text := formatKnowledgeBaseList([]*KnowledgeBaseInfo{
		{ID: "kb-1", Name: "Ops", Profile: &types.KnowledgeBaseProfile{Status: types.KnowledgeBaseProfileStatusEmpty}},
		{ID: "kb-2", Name: "Docs"},
	})
	require.NotContains(t, text, "<generated_profile>")
}

func compactDirectoryFixture() []*KnowledgeBaseInfo {
	return []*KnowledgeBaseInfo{
		{
			ID: "kb-1", Name: "Pháp luật", Type: "document", DocCount: 42, Capabilities: []string{"vector", "keyword"},
			Description: strings.Repeat("Văn bản quy phạm pháp luật về hành chính. ", 10),
			Profile: &types.KnowledgeBaseProfile{
				Gist:             "Luật, nghị định & thông tư",
				Topics:           []string{"đất đai", "xây dựng"},
				TypicalQuestions: []string{"Thời hạn cấp giấy chứng nhận?"},
			},
			RecentDocs: []RecentDocInfo{{KnowledgeID: "k-1", Title: "Nghị định 30"}},
		},
		{
			ID: "kb-2", Name: "Hướng dẫn <nội bộ>",
			Profile: &types.KnowledgeBaseProfile{Gist: "Quy trình nội bộ của Sở", Topics: []string{"quy trình"}},
		},
	}
}

// A document-assistant turn that does not retrieve lists each base by its
// identity and the opening of its description; the full directory keeps the
// generated profile and recent documents.
func TestFormatKnowledgeBaseDirectoryCompactAndFull(t *testing.T) {
	kbs := compactDirectoryFixture()

	full := formatKnowledgeBaseDirectory(kbs, false)
	require.Equal(t, formatKnowledgeBaseList(kbs), full)
	require.Contains(t, full, "<generated_profile>")
	require.Contains(t, full, "<typical_questions>")
	require.Contains(t, full, "<recent_documents>")
	require.NotContains(t, full, compactKnowledgeDirectoryNote)

	compact := formatKnowledgeBaseDirectory(kbs, true)
	require.Contains(t, compact, `<knowledge_base id="kb-1" name="Pháp luật" type="document" doc_count="42" capabilities="vector,keyword">`)
	require.Contains(t, compact, `<knowledge_base id="kb-2" name="Hướng dẫn &lt;nội bộ&gt;" type="document" doc_count="0" capabilities="">`)
	require.NotContains(t, compact, "<generated_profile>")
	require.NotContains(t, compact, "<topics>")
	require.NotContains(t, compact, "<question>")
	require.NotContains(t, compact, "<recent_documents>")
	require.Contains(t, compact, "search_knowledge searches all of these knowledge bases in one call")
	// the description is cut to its opening; without one the gist stands in
	require.Contains(t, compact, "...</description>")
	require.NotContains(t, compact, strings.Repeat("Văn bản quy phạm pháp luật về hành chính. ", 6))
	require.Contains(t, compact, "<description>Quy trình nội bộ của Sở</description>")
	require.Less(t, len(compact), len(full))
	decoder := xml.NewDecoder(strings.NewReader(compact))
	for {
		_, err := decoder.Token()
		if err == io.EOF {
			break
		}
		require.NoError(t, err, "the compact directory stays well-formed")
	}

	require.Equal(t, "<knowledge_bases />", formatKnowledgeBaseDirectory(nil, true))
}

// Only a document-assistant turn that does not start with knowledge
// retrieval gets the compact directory.
func TestCompactKnowledgeDirectoryOnlyForDocumentAssistantWithoutRetrieval(t *testing.T) {
	ctx := context.Background()
	require.False(t, compactKnowledgeDirectory(ctx), "other agents keep the full directory")
	require.False(t, compactKnowledgeDirectory(types.WithFirstToolChoice(ctx, tools.ToolSearchKnowledge)))

	doc := types.WithDocumentAssistantTurn(ctx)
	require.True(t, compactKnowledgeDirectory(doc))
	require.True(t, compactKnowledgeDirectory(types.WithFirstToolChoice(doc, tools.ToolRewriteParagraphs)))
	require.False(t, compactKnowledgeDirectory(types.WithFirstToolChoice(doc, tools.ToolSearchKnowledge)))
	require.False(t, compactKnowledgeDirectory(types.WithGroundingRequired(doc)))

	block := buildRuntimeContextBlock("s", compactDirectoryFixture(), nil, nil, compactKnowledgeDirectory(doc))
	require.Contains(t, block, compactKnowledgeDirectoryNote[:20])
	require.NotContains(t, block, "<generated_profile>")
	block = buildRuntimeContextBlock("s", compactDirectoryFixture(), nil, nil, false)
	require.Contains(t, block, "<generated_profile>")
}
