package im

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

// HelpCommand implements /help [command].
type HelpCommand struct {
	registry *CommandRegistry
}

func newHelpCommand(registry *CommandRegistry) *HelpCommand {
	return &HelpCommand{registry: registry}
}

func (c *HelpCommand) Name() string { return "help" }
func (c *HelpCommand) Description() string {
	return "Hiển thị danh sách lệnh, hoặc xem cách dùng chi tiết của một lệnh"
}

func (c *HelpCommand) Execute(_ context.Context, _ *CommandContext, args []string) (*CommandResult, error) {
	// /help <command> — show detailed usage for a specific command
	if len(args) > 0 {
		name := strings.ToLower(args[0])
		cmd, _, ok := c.registry.Parse("/" + name)
		if !ok {
			return &CommandResult{
				Content: fmt.Sprintf("Lệnh không xác định `%s`, gửi `/help` để xem tất cả lệnh.", args[0]),
			}, nil
		}
		return &CommandResult{
			Content: fmt.Sprintf("**/%s** — %s", cmd.Name(), cmd.Description()),
		}, nil
	}

	// /help — list all commands sorted by name
	cmds := c.registry.All()
	sort.Slice(cmds, func(i, j int) bool { return cmds[i].Name() < cmds[j].Name() })

	var sb strings.Builder
	sb.WriteString("**Các lệnh khả dụng**\n\n")
	for _, cmd := range cmds {
		sb.WriteString(fmt.Sprintf("· `/%s` — %s\n", cmd.Name(), cmd.Description()))
	}
	sb.WriteString("\nGửi `/help <tên lệnh>` để xem cách dùng chi tiết")
	return &CommandResult{Content: sb.String()}, nil
}
