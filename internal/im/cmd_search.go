package im

import (
	"context"
	"fmt"
	"strings"

	"github.com/Tencent/WeKnora/internal/agent/tools"
	"github.com/Tencent/WeKnora/internal/application/access"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

const (
	searchMaxResults    = 5
	searchContentMaxLen = 200 // runes shown per result
)

// SearchCommand implements /search <query>.
//
// It runs a hybrid search (vector + keywords) against the user's selected
// knowledge bases—or the bot-level defaults when no override is active—and
// returns the raw matching passages without AI summarisation. This is useful
// when the user needs to inspect source text directly.
type SearchCommand struct {
	sessionService interfaces.SessionService
	kbService      interfaces.KnowledgeBaseService
}

func newSearchCommand(sessionService interfaces.SessionService, kbService interfaces.KnowledgeBaseService) *SearchCommand {
	return &SearchCommand{sessionService: sessionService, kbService: kbService}
}

func (c *SearchCommand) Name() string { return "search" }
func (c *SearchCommand) Description() string {
	return "Tra cứu trực tiếp nguyên văn trong kho tri thức (không qua AI tóm tắt), ví dụ: /search chính sách hoàn tiền"
}

func (c *SearchCommand) Execute(ctx context.Context, cmdCtx *CommandContext, args []string) (*CommandResult, error) {
	if len(args) == 0 {
		return &CommandResult{
			Content: "Vui lòng nhập nội dung tìm kiếm, ví dụ: `/search chính sách hoàn tiền`",
		}, nil
	}

	query := strings.Join(args, " ")

	// Resolve which KBs to search, mirroring the logic in the QA pipeline's
	// resolveKnowledgeBasesFromAgent so that /search covers the same scope.
	var kbIDs []string
	if cmdCtx.CustomAgent != nil {
		switch cmdCtx.CustomAgent.Config.KBSelectionMode {
		case "all":
			allKBs, err := c.kbService.ListKnowledgeBases(ctx)
			if err == nil {
				// Same capability filter as the QA pipeline's
				// resolveKnowledgeBasesFromAgent (`all` branch) so `/search`
				// agrees with what the agent's tools can actually reach.
				// Agent-mode aware: quick-answer enforces RAG-only KBs.
				agentMode := cmdCtx.CustomAgent.Config.AgentMode
				allowed := cmdCtx.CustomAgent.Config.AllowedTools
				filter := tools.DeriveKBFilterForAgent(agentMode, allowed)
				skipped := 0
				caller := types.CallerFromContext(ctx)
				for _, kb := range allKBs {
					if !access.InDefaultRetrievalScope(caller, kb) {
						continue
					}
					if !filter.IsEmpty() &&
						!tools.KBSatisfiesAgentRequirements(kb.Capabilities(), agentMode, allowed) {
						skipped++
						continue
					}
					kbIDs = append(kbIDs, kb.ID)
				}
				if skipped > 0 {
					logger.Infof(ctx,
						"/search(agent=%s, mode=all): capability filter removed %d of %d KBs",
						cmdCtx.CustomAgent.ID, skipped, len(allKBs))
				}
			}
		case types.KBSelectionModePublic:
			// Platform public knowledge bases only, as the QA pipeline.
			if allKBs, err := c.kbService.ListKnowledgeBases(ctx); err == nil {
				for _, kb := range allKBs {
					if access.IsPlatformPublicKB(kb) {
						kbIDs = append(kbIDs, kb.ID)
					}
				}
			}
		case "none":
			// No knowledge bases configured — will return empty results.
		case "selected":
			kbIDs = cmdCtx.CustomAgent.Config.KnowledgeBases
		default:
			// Backward compatibility: fall back to configured list.
			kbIDs = cmdCtx.CustomAgent.Config.KnowledgeBases
		}
	}

	results, err := c.sessionService.SearchKnowledge(ctx, kbIDs, nil, nil, query)
	if err != nil {
		return nil, fmt.Errorf("search knowledge: %w", err)
	}

	if len(results) == 0 {
		return &CommandResult{
			Content: fmt.Sprintf("Không tìm thấy nội dung liên quan đến “%s” trong kho tri thức.", query),
		}, nil
	}

	// Cap the number of results shown in IM (wall of text is unhelpful).
	shown := results
	if len(shown) > searchMaxResults {
		shown = shown[:searchMaxResults]
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("🔍 **Tìm “%s”** — tìm thấy %d kết quả\n\n", query, len(results)))

	for i, r := range shown {
		// Trim content to a readable length.
		content := []rune(r.Content)
		suffix := ""
		if len(content) > searchContentMaxLen {
			content = content[:searchContentMaxLen]
			suffix = "…"
		}

		// Source label: prefer title, fall back to filename.
		source := r.KnowledgeTitle
		if source == "" {
			source = r.KnowledgeFilename
		}

		sb.WriteString(fmt.Sprintf("**[%d]** %s\n> %s%s\n", i+1, source, string(content), suffix))

		if r.Score > 0 {
			sb.WriteString(fmt.Sprintf("Độ khớp: %.0f%%\n", r.Score*100))
		}
		sb.WriteString("\n")
	}

	if len(results) > searchMaxResults {
		sb.WriteString(fmt.Sprintf("_(chỉ hiển thị %d kết quả đầu, tổng %d)_", searchMaxResults, len(results)))
	}

	return &CommandResult{Content: sb.String()}, nil
}
