package im

import "context"

// StopCommand implements /stop.
// It cancels the in-flight QA request for the current user+chat, allowing the
// user to abort a long-running ReAct reasoning chain without waiting for it to
// complete. If no request is in progress the command simply acknowledges.
type StopCommand struct{}

func newStopCommand() *StopCommand { return &StopCommand{} }

func (c *StopCommand) Name() string        { return "stop" }
func (c *StopCommand) Description() string { return "Dừng câu trả lời đang được tạo" }

func (c *StopCommand) Execute(_ context.Context, _ *CommandContext, _ []string) (*CommandResult, error) {
	return &CommandResult{
		Content: "✅ Đã yêu cầu dừng câu trả lời hiện tại.",
		Action:  ActionStop,
	}, nil
}
