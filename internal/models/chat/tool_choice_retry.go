package chat

import "strings"

// Some providers refuse a forced tool_choice while the model is in thinking
// mode. Gemini and DeepSeek-style backends answer HTTP 400 with messages such
// as "Thinking mode does not support this tool_choice" instead of ignoring the
// field. The round is still useful when the model picks the tool itself, so
// Chat/ChatStream retry it once without the forced name.
//
// The first agent round forces a tool on purpose (types.WithFirstToolChoice)
// and turns thinking off for that round; a model that cannot leave thinking
// mode (deepseek-reasoner, provider-side defaults) would otherwise fail the
// whole turn here.

// isThinkingToolChoiceError reports a rejection of the forced tool_choice
// caused by thinking mode.
func isThinkingToolChoiceError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "thinking mode") && strings.Contains(msg, "tool_choice")
}

// forcedToolChoice reports whether opts pins one specific tool. Empty, "auto"
// and "none" leave the choice to the model (or drop tools) and are legal in
// thinking mode.
func forcedToolChoice(opts *ChatOptions) bool {
	if opts == nil {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(opts.ToolChoice)) {
	case "", "auto", "none":
		return false
	}
	return true
}

// withoutForcedToolChoice returns a copy of opts that lets the model choose
// the tool. The caller's options are not mutated.
func withoutForcedToolChoice(opts *ChatOptions) *ChatOptions {
	if opts == nil {
		return nil
	}
	retry := *opts
	retry.ToolChoice = ""
	return &retry
}
