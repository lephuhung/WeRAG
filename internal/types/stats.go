package types

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

// SystemStats is the response payload of GET /system/admin/stats.
type SystemStats struct {
	Accounts  AccountStats  `json:"accounts"`
	Documents DocumentStats `json:"documents"`
	Messages  MessageStats  `json:"messages"`
}
