package types

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"time"
)

// Durable clarification-turn state for the abbreviation-resolution gate.
// The turn lifecycle (inspecting → awaiting_definition → …) is persisted per
// owner scope so at most one awaiting_definition turn per session resumes
// automatically; the typed payload reuses the Task-1 resolution DTOs.
// All persisted fields carry explicit snake_case JSON tags.

// Turn states stored in abbreviation_turn_states.state.
const (
	AbbreviationTurnInspecting         = "inspecting"
	AbbreviationTurnAwaitingDefinition = "awaiting_definition"
	AbbreviationTurnReady              = "ready"
	AbbreviationTurnRunning            = "running"
	AbbreviationTurnCompleted          = "completed"
	AbbreviationTurnBlockedError       = "blocked_error"
	AbbreviationTurnCancelled          = "cancelled"
	AbbreviationTurnExpired            = "expired"
)

// Message-link roles stored in abbreviation_turn_messages.role.
const (
	AbbreviationMessageRoleRoot          = "root"
	AbbreviationMessageRoleDefinition    = "definition"
	AbbreviationMessageRoleClarification = "clarification"
	AbbreviationMessageRoleAnswer        = "answer"
)

// AbbreviationRequestSnapshot freezes the QA request scope a turn was
// prepared from, so resumption replays the same retrieval surface.
type AbbreviationRequestSnapshot struct {
	AgentID             string     `json:"agent_id"`
	Mode                string     `json:"mode"`
	AgentTenantID       uint64     `json:"agent_tenant_id"`
	KnowledgeBaseIDs    []string   `json:"knowledge_base_ids"`
	KnowledgeIDs        []string   `json:"knowledge_ids"`
	MCPServiceIDs       []string   `json:"mcp_service_ids"`
	SkillNames          []string   `json:"skill_names"`
	TagScopes           []TagScope `json:"tag_scopes"`
	WebSearchEnabled    bool       `json:"web_search_enabled"`
	LocalBrowserEnabled bool       `json:"local_browser_enabled"`
	AttachmentIDs       []string   `json:"attachment_ids"`
	Locale              string     `json:"locale"`
}

// AbbreviationTurnPayload is the persisted turn content: the frozen request
// snapshot plus the live Task-1 resolution. It implements driver.Valuer and
// sql.Scanner for the JSONB (PostgreSQL) / TEXT JSON (SQLite) column; the
// JSON round-trip yields fresh slices, so scanned payloads never alias.
type AbbreviationTurnPayload struct {
	Snapshot   AbbreviationRequestSnapshot `json:"snapshot"`
	Resolution AbbreviationResolution      `json:"resolution"`
}

// Value marshals the payload for storage.
func (p AbbreviationTurnPayload) Value() (driver.Value, error) {
	raw, err := json.Marshal(p)
	if err != nil {
		return nil, err
	}
	return string(raw), nil
}

// Scan decodes a stored payload into a fresh value that replaces the
// receiver only after successful decoding. JSON null, empty objects and
// omitted fields therefore reset the payload instead of retaining previous
// data; malformed or unsupported values leave the receiver untouched, and
// decoded slices never alias a previously scanned payload.
func (p *AbbreviationTurnPayload) Scan(src any) error {
	if p == nil {
		return fmt.Errorf("scan into nil AbbreviationTurnPayload")
	}
	var raw []byte
	switch v := src.(type) {
	case nil:
		*p = AbbreviationTurnPayload{}
		return nil
	case string:
		raw = []byte(v)
	case []byte:
		raw = v
	default:
		return fmt.Errorf("unsupported AbbreviationTurnPayload source %T", src)
	}
	var fresh AbbreviationTurnPayload
	if err := json.Unmarshal(raw, &fresh); err != nil {
		return err
	}
	*p = fresh
	return nil
}

// Clone deep-copies the payload: snapshot slices, tag scopes and every
// resolution descendant (occurrences, meanings, definition evidence) are
// freshly allocated, so sibling query results never alias each other.
func (p AbbreviationTurnPayload) Clone() AbbreviationTurnPayload {
	out := p
	snap := p.Snapshot
	snap.KnowledgeBaseIDs = append([]string(nil), snap.KnowledgeBaseIDs...)
	snap.KnowledgeIDs = append([]string(nil), snap.KnowledgeIDs...)
	snap.MCPServiceIDs = append([]string(nil), snap.MCPServiceIDs...)
	snap.SkillNames = append([]string(nil), snap.SkillNames...)
	snap.AttachmentIDs = append([]string(nil), snap.AttachmentIDs...)
	snap.TagScopes = make([]TagScope, len(snap.TagScopes))
	for i, scope := range p.Snapshot.TagScopes {
		cp := scope
		cp.TagIDs = append([]string(nil), scope.TagIDs...)
		snap.TagScopes[i] = cp
	}
	out.Snapshot = snap
	res := p.Resolution
	res.UnknownTerms = append([]string(nil), res.UnknownTerms...)
	res.Terms = make([]AbbreviationTerm, len(res.Terms))
	for i, term := range p.Resolution.Terms {
		cp := term
		cp.Occurrences = append([]AbbreviationOccurrence(nil), term.Occurrences...)
		cp.Meanings = append([]AbbreviationMeaning(nil), term.Meanings...)
		if term.Definition != nil {
			def := *term.Definition
			cp.Definition = &def
		}
		res.Terms[i] = cp
	}
	out.Resolution = res
	return out
}

// AbbreviationTurnState is one persisted clarification turn. Owner scope
// (tenant/session/owner/principal) appears on every operation; reads filter
// the full scope, never the bare ID.
type AbbreviationTurnState struct {
	ID                     string                  `json:"id"`
	TenantID               uint64                  `json:"tenant_id"`
	SessionID              string                  `json:"session_id"`
	OwnerID                string                  `json:"owner_id"`
	PrincipalID            string                  `json:"principal_id"`
	RootUserMessageID      string                  `json:"root_user_message_id"`
	ClarificationMessageID string                  `json:"clarification_message_id"`
	ExecutingMessageID     string                  `json:"executing_message_id"`
	State                  string                  `json:"state"`
	ErrorCode              string                  `json:"error_code"`
	Version                uint64                  `json:"version"`
	Payload                AbbreviationTurnPayload `json:"payload"`
	CreatedAt              time.Time               `json:"created_at"`
	UpdatedAt              time.Time               `json:"updated_at"`
	ExpiresAt              time.Time               `json:"expires_at"`
}

// TableName returns the turn-state table name.
func (AbbreviationTurnState) TableName() string { return "abbreviation_turn_states" }

// AbbreviationMessageLink binds one message to its turn: the message ID is
// the primary key, the request ID the turn foreign key.
type AbbreviationMessageLink struct {
	MessageID string `json:"message_id"`
	RequestID string `json:"request_id"`
	Role      string `json:"role"`
}

// TableName returns the message-link table name.
func (AbbreviationMessageLink) TableName() string { return "abbreviation_turn_messages" }
