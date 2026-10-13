package types

import "time"

// AccountStats aggregates platform-wide account counters for the
// SystemAdmin dashboard. All counts exclude soft-deleted rows.
type AccountStats struct {
	// TotalUsers is the number of user accounts in the system.
	TotalUsers int64 `json:"total_users"`
	// ActiveUsers counts users with is_active = true.
	ActiveUsers int64 `json:"active_users"`
	// InactiveUsers counts users with is_active = false.
	InactiveUsers int64 `json:"inactive_users"`
	// SystemAdmins counts users holding the system-admin flag.
	SystemAdmins int64 `json:"system_admins"`
	// TotalTenants is the number of workspaces.
	TotalTenants int64 `json:"total_tenants"`
	// NewUsersLast30d counts users created in the trailing 30 days.
	NewUsersLast30d int64 `json:"new_users_last_30d"`
}

// DocumentStatusCount is one row of a parse_status GROUP BY over the
// knowledges table.
type DocumentStatusCount struct {
	Status string `json:"status"`
	Count  int64  `json:"count"`
}

// DocumentStats aggregates document (knowledge) counters.
type DocumentStats struct {
	// Total is the number of non-deleted knowledge entries.
	Total int64 `json:"total"`
	// ByStatus breaks documents down by parse_status
	// (pending/processing/finalizing/completed/failed/deleting/cancelled).
	ByStatus []DocumentStatusCount `json:"by_status"`
}

// MessageDayActivity is the per-day message count used by the activity
// heatmap.
type MessageDayActivity struct {
	// Date is the calendar day in YYYY-MM-DD (server local time of the DB).
	Date string `json:"date"`
	// Count is total messages (all roles) created that day.
	Count int64 `json:"count"`
	// UserCount counts role='user' messages (serialised as "user").
	UserCount int64 `json:"user"`
	// AssistantCount counts role='assistant' messages (serialised as "assistant").
	AssistantCount int64 `json:"assistant"`
}

// MessageStats aggregates chat activity counters.
type MessageStats struct {
	// Total is the number of non-deleted messages overall.
	Total int64 `json:"total"`
	// TotalSessions is the number of non-deleted sessions overall.
	TotalSessions int64 `json:"total_sessions"`
	// Days is the trailing window covered by ByDay (e.g. 90).
	Days int `json:"days"`
	// ByDay holds one entry per day with at least one message, oldest first.
	// Days without activity are omitted — the frontend renders gaps as zero.
	ByDay []MessageDayActivity `json:"by_day"`
}

// ChatWindowStats counts chat activity over a trailing window (24 hours).
type ChatWindowStats struct {
	// Hours is the length of the trailing window.
	Hours int `json:"hours"`
	// Questions counts role='user' messages — one per chat turn.
	Questions int64 `json:"questions"`
	// Answers counts role='assistant' messages.
	Answers int64 `json:"answers"`
	// ActiveSessions counts distinct sessions with a message in the window.
	ActiveSessions int64 `json:"active_sessions"`
	// ActiveUsers counts distinct session owners with a message in the window.
	ActiveUsers int64 `json:"active_users"`
}

// FeedbackStats counts like/dislike votes on assistant answers.
type FeedbackStats struct {
	Likes    int64 `json:"likes"`
	Dislikes int64 `json:"dislikes"`
	// Likes24h / Dislikes24h count votes cast (feedback_at) in the last 24h.
	Likes24h    int64 `json:"likes_24h"`
	Dislikes24h int64 `json:"dislikes_24h"`
}

// PublicKBDocumentCount is the document breakdown of one platform-public
// knowledge base.
type PublicKBDocumentCount struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Type       string `json:"type"`
	Total      int64  `json:"total"`
	Completed  int64  `json:"completed"`
	Processing int64  `json:"processing"`
	Failed     int64  `json:"failed"`
}

// SystemStats is the response payload of GET /system/admin/stats.
type SystemStats struct {
	Accounts  AccountStats    `json:"accounts"`
	Documents DocumentStats   `json:"documents"`
	Messages  MessageStats    `json:"messages"`
	Chat24h   ChatWindowStats `json:"chat_24h"`
	Feedback  FeedbackStats   `json:"feedback"`
	// PublicKBs lists every platform-public knowledge base with its
	// document counts, largest first.
	PublicKBs []PublicKBDocumentCount `json:"public_kbs"`
	// Usage sums every workspace's questions and tokens for today and
	// this month (Vietnam calendar).
	Usage UsageStats `json:"usage"`
}

// ProcessingDocument is one document still in the ingest pipeline.
type ProcessingDocument struct {
	ID                   string    `json:"id"`
	Title                string    `json:"title"`
	FileName             string    `json:"file_name"`
	FileType             string    `json:"file_type"`
	ParseStatus          string    `json:"parse_status"`
	PendingSubtasksCount int       `json:"pending_subtasks_count"`
	KnowledgeBaseID      string    `json:"knowledge_base_id"`
	KnowledgeBaseName    string    `json:"knowledge_base_name"`
	TenantID             uint64    `json:"tenant_id"`
	TenantName           string    `json:"tenant_name"`
	CreatedAt            time.Time `json:"created_at"`
	UpdatedAt            time.Time `json:"updated_at"`
}

// ProcessingDocuments is the response payload of
// GET /system/admin/stats/processing.
type ProcessingDocuments struct {
	// Total counts every pending/processing/finalizing document; Items holds
	// at most the requested limit, oldest first.
	Total int64                `json:"total"`
	Items []ProcessingDocument `json:"items"`
	// FailedLast24h counts documents that failed in the last 24 hours.
	FailedLast24h int64 `json:"failed_24h"`
}
