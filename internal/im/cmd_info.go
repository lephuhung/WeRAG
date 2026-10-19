package im

import (
	"context"
	"fmt"
	"strings"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

// InfoCommand implements /info.
// It shows the bound agent's profile and capabilities so IM users can
// understand what the bot can do without leaving the chat.
type InfoCommand struct {
	kbService interfaces.KnowledgeBaseService
}

func newInfoCommand(kbService interfaces.KnowledgeBaseService) *InfoCommand {
	return &InfoCommand{kbService: kbService}
}

func (c *InfoCommand) Name() string { return "info" }
func (c *InfoCommand) Description() string {
	return "Xem thông tin và khả năng của trợ lý hiện tại"
}

func (c *InfoCommand) Execute(ctx context.Context, cmdCtx *CommandContext, _ []string) (*CommandResult, error) {
	var sb strings.Builder

	// Note: Feishu card markdown only renders **bold** when it occupies the
	// entire inline segment. "**label：**value" on the same line will show
	// raw asterisks. Always keep bold text self-contained on its own line.

	// ── Header ──
	name := cmdCtx.AgentName
	if name == "" {
		name = "Trợ lý chưa đặt tên"
	}
	sb.WriteString(fmt.Sprintf("🤖 **%s**\n", name))
	if cmdCtx.CustomAgent != nil && cmdCtx.CustomAgent.Description != "" {
		sb.WriteString(fmt.Sprintf("> %s\n", cmdCtx.CustomAgent.Description))
	}

	if cmdCtx.CustomAgent == nil {
		sb.WriteString("\nChưa gắn trợ lý, gửi `/help` để xem các lệnh.")
		return &CommandResult{Content: sb.String()}, nil
	}

	cfg := cmdCtx.CustomAgent.Config

	// ── Mode ──
	if cmdCtx.CustomAgent.IsAgentMode() {
		sb.WriteString("\n🧠 **Chế độ trợ lý**\n")
		sb.WriteString("Hỗ trợ suy nghĩ nhiều bước, gọi công cụ (ReAct)\n")
	} else {
		sb.WriteString("\n🧠 **Chế độ trợ lý**\n")
		sb.WriteString("Trả lời trực tiếp dựa trên tra cứu kho tri thức (RAG)\n")
	}

	// ── Knowledge bases ──
	// KBSelectionMode: "all" uses every KB under the tenant (IDs list is empty),
	// "selected" uses the explicit KnowledgeBases list, "none"/empty means disabled.
	sb.WriteString("\n📚 **Kho tri thức**\n")
	if cfg.KBSelectionMode == types.KBSelectionModePublic {
		sb.WriteString("  Các kho tri thức công khai của nền tảng\n")
	} else if cfg.KBSelectionMode == "all" {
		kbs, err := c.kbService.ListKnowledgeBasesByTenantID(ctx, cmdCtx.TenantID)
		if err == nil && len(kbs) > 0 {
			for _, kb := range kbs {
				sb.WriteString(fmt.Sprintf("  · %s\n", kb.Name))
			}
			sb.WriteString(fmt.Sprintf("  Tổng %d kho (bật tất cả)\n", len(kbs)))
		} else {
			sb.WriteString("  Bật tất cả\n")
		}
	} else if len(cfg.KnowledgeBases) > 0 {
		kbs, err := c.kbService.ListKnowledgeBasesByTenantID(ctx, cmdCtx.TenantID)
		if err == nil {
			nameMap := make(map[string]string, len(kbs))
			for _, kb := range kbs {
				nameMap[kb.ID] = kb.Name
			}
			for _, id := range cfg.KnowledgeBases {
				label := id
				if n, ok := nameMap[id]; ok {
					label = n
				}
				sb.WriteString(fmt.Sprintf("  · %s\n", label))
			}
		} else {
			sb.WriteString(fmt.Sprintf("  Đã chọn %d\n", len(cfg.KnowledgeBases)))
		}
	} else {
		sb.WriteString("  Chưa cấu hình\n")
	}

	// ── Skills ──
	sb.WriteString("\n⚡ **Skills**\n")
	if cfg.SkillsSelectionMode == "all" {
		sb.WriteString("  Bật tất cả\n")
	} else if cfg.SkillsSelectionMode == "selected" && len(cfg.SelectedSkills) > 0 {
		for _, s := range cfg.SelectedSkills {
			sb.WriteString(fmt.Sprintf("  · %s\n", s))
		}
	} else {
		sb.WriteString("  Chưa cấu hình\n")
	}

	// ── MCP ──
	sb.WriteString("\n🔌 **Dịch vụ MCP**\n")
	if cfg.MCPSelectionMode == "all" {
		sb.WriteString("  Kết nối tất cả\n")
	} else if cfg.MCPSelectionMode == "selected" && len(cfg.MCPServices) > 0 {
		sb.WriteString(fmt.Sprintf("  Đã kết nối %d dịch vụ\n", len(cfg.MCPServices)))
	} else {
		sb.WriteString("  Chưa cấu hình\n")
	}

	// ── Web search ──
	sb.WriteString("\n🌐 **Tìm kiếm web**\n")
	if cfg.WebSearchEnabled {
		sb.WriteString("  Đã bật\n")
	} else {
		sb.WriteString("  Chưa bật\n")
	}

	// ── Footer ──
	outputLabel := "Xuất theo luồng"
	if cmdCtx.ChannelOutputMode == "full" {
		outputLabel = "Xuất trọn vẹn"
	}
	sb.WriteString(fmt.Sprintf("\n⚙️ **Chế độ xuất**\n  %s\n", outputLabel))
	sb.WriteString("\n---\nGửi `/help` để xem tất cả lệnh")

	return &CommandResult{Content: sb.String()}, nil
}
