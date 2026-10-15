package types

// Values of SessionContextUsage.Mode.
const (
	ContextUsageModeAgent       = "agent"
	ContextUsageModeQuickAnswer = "quick-answer"
)

// SessionContextUsage is how full the conversation history of a session
// is, as its next turn would load it, next to the caps of the caller's
// plan. The chat UI's context indicator polls it after each turn.
type SessionContextUsage struct {
	// ModelWindow is the context window of the session's chat model.
	ModelWindow int `json:"model_window"`
	// HistoryBudget is how much history a turn may carry before older
	// turns are summarized: the plan's context.history_tokens clamped, or
	// ModelWindow when no cap applies.
	HistoryBudget int `json:"history_budget"`
	// HistoryTokens is the estimated history of the next turn: the summary
	// plus the turns after it, priced as the history loader prices them.
	HistoryTokens int `json:"history_tokens"`
	// SummaryTokens is the part of HistoryTokens that is a summary.
	SummaryTokens int `json:"summary_tokens"`
	// Compacted: the history starts from a summary of earlier turns.
	Compacted bool `json:"compacted"`
	// CompactedTurns is how many turns of this session the summary covers.
	CompactedTurns int `json:"compacted_turns"`
	// Seeded: the summary is the one of the session this one continues
	// (this session has no compaction of its own yet).
	Seeded bool `json:"seeded"`
	// Mode is the history shape estimated (ContextUsageMode*).
	Mode string `json:"mode"`
	// HistoryRounds is how many recent rounds a quick-answer turn carries
	// (fewer after a summary); 0 in agent mode, which sizes history in
	// tokens.
	HistoryRounds int `json:"history_rounds"`
	// LastTurn is the newest completed turn's usage, nil before any.
	LastTurn *SessionContextLastTurn `json:"last_turn"`
	// Session is the session's lifetime usage against the plan's caps.
	Session SessionContextSessionStatus `json:"session"`
	// TurnTokenCap is the plan's agent.turn_token_cap, 0 for none.
	TurnTokenCap int64 `json:"turn_token_cap"`
	// PlanName is the governing package's name, "" when none applies.
	PlanName string `json:"plan_name"`
	// Enforced: the plan's caps apply to this caller (usage.enforce is on
	// and the caller is not a SuperAdmin).
	Enforced bool `json:"enforced"`
	// Denied: enforcement is on and the caller's package would refuse the
	// next turn; DenialReason says why (UsageDenial* reasons:
	// no_subscription, expired, no_seat, limit_exhausted).
	Denied       bool   `json:"denied"`
	DenialReason string `json:"reason,omitempty"`
}

// SessionContextLastTurn is the token usage of one turn, summed over its
// model calls.
type SessionContextLastTurn struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	// Rounds is how many model rounds the turn ran.
	Rounds int `json:"rounds"`
}

// SessionContextSessionStatus is UsageSessionStatus for display: the
// counters are filled even when the plan sets no cap (0 = no cap).
type SessionContextSessionStatus struct {
	Turns     int64 `json:"turns"`
	MaxTurns  int64 `json:"max_turns"`
	Tokens    int64 `json:"tokens"`
	MaxTokens int64 `json:"max_tokens"`
	Warning   bool  `json:"warning"`
	Reached   bool  `json:"reached"`
	Soft      bool  `json:"soft"`
}

// UsageSessionPreview is what the usage guard would grant a turn of a
// session, resolved without counting, creating or refusing anything.
type UsageSessionPreview struct {
	// Enforced: the plan's caps apply (usage.enforce on, not a SuperAdmin).
	Enforced bool
	// PlanName is the governing package's name, "" for none.
	PlanName string
	// Denied: Enforced, and CheckTurn would refuse the caller's package
	// (none, expired, no seat, or a used-up limit); DenialReason is the
	// UsageDenial reason. A session cap is reported in Session instead.
	Denied       bool
	DenialReason string
	// Features are the caps that apply; zero when not Enforced.
	Features UsagePlanFeatures
	// Session is the session's lifetime counters against those caps; its
	// caps are 0 when the plan sets none.
	Session UsageSessionStatus
}
