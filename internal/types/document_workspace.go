package types

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// Document workspace statuses. A workspace is "open" while a session's
// document can still be edited in the embedded editor; it becomes "closed"
// once the editor reports the final save after the last user left.
const (
	DocumentWorkspaceStatusOpen   = "open"
	DocumentWorkspaceStatusClosed = "closed"
)

// Document roles. A target is the Word file open in an editor tab: the only
// document the format check and the editing tools work on. A source is a
// file uploaded at chat (docx, pdf, xlsx, a scan, …): it has no editor tab
// and is only looked up, through the text parsed from the upload. The role
// comes from how the file was opened, not from upload order; a Word source
// can be promoted to a target and a target demoted to a source.
const (
	DocumentWorkspaceRoleTarget = "target"
	DocumentWorkspaceRoleSource = "source"
)

// Text statuses of a source: its parsed text is copied from the upload once
// parsing finishes (TextStatus is "" for a target).
const (
	DocumentSourceTextProcessing = "processing"
	DocumentSourceTextReady      = "ready"
	DocumentSourceTextFailed     = "failed"
)

// Limits of the editable documents of one session. A document is a Word
// file the user edits, so heavy embedded pictures (scans pasted into the
// body) are refused: they slow the editor and every AI read, and a seal or
// signature image is far below the media cap.
const (
	MaxDocumentWorkspacesPerSession = 4
	// MaxDocumentSourcesPerSession caps the sources (chat uploads) of a
	// session; they share the vb1…vbN handles with the targets.
	MaxDocumentSourcesPerSession   = 10
	MaxDocumentWorkspaceFileBytes  = 10 << 20
	MaxDocumentWorkspaceMediaBytes = 3 << 20
	// DocumentHandlePrefix + position names a document for the agent.
	DocumentHandlePrefix = "vb"
	// MentionTypeDocument is the @mention type of an editable document
	// (MentionedItem.ID is its workspace ID).
	MentionTypeDocument = "document"
)

// DocumentWorkspace is one document of a chat session in the
// document-assistant agent. A target is an editable .docx, one editor tab
// each, at most MaxDocumentWorkspacesPerSession; OriginalRef is the uploaded
// copy and CurrentRef the latest version, and the snapshot timeline
// (DocumentRevision) keeps the versions the user can restore. A source (at
// most MaxDocumentSourcesPerSession) keeps a copy of the uploaded file in
// OriginalRef = CurrentRef and its parsed text in DocumentWorkspaceText.
type DocumentWorkspace struct {
	ID        string `json:"id" gorm:"type:varchar(36);primaryKey"`
	TenantID  uint64 `json:"tenant_id" gorm:"not null;index"`
	SessionID string `json:"session_id" gorm:"type:varchar(36);not null;index"`
	// AttachmentID is the session upload the document was opened from.
	AttachmentID string `json:"attachment_id" gorm:"type:varchar(36);not null;default:''"`
	// Position is the 1-based order the document was opened in within the
	// session, never reused; the agent names the document vb<Position>.
	Position int `json:"position" gorm:"not null;default:1"`
	// ActiveAt is when the user last switched to the document's tab; the
	// most recent one is the session's active document.
	ActiveAt    *time.Time `json:"active_at,omitempty"`
	UserID      string     `json:"user_id" gorm:"type:varchar(36);not null;default:''"`
	OriginalRef string     `json:"-" gorm:"type:text;not null"`
	CurrentRef  string     `json:"-" gorm:"type:text;not null"`
	FileName    string     `json:"file_name" gorm:"type:varchar(1024);not null"`
	// FileType is "docx" for a target: a legacy .doc upload is converted
	// when the target is created so every tool works on OOXML. A source
	// keeps the upload's extension without the dot (pdf, xlsx, doc, …).
	FileType string `json:"file_type" gorm:"type:varchar(16);not null;default:'docx'"`
	FileSize int64  `json:"file_size" gorm:"not null;default:0"`
	// Revision increases every time CurrentRef is replaced by bytes written
	// outside the editor (an AI edit). The editor key is derived from it, so
	// the editor reloads exactly when the file changed under it.
	Revision int    `json:"revision" gorm:"not null;default:0"`
	Status   string `json:"status" gorm:"type:varchar(16);not null;index"`
	// SaveCount counts editor saves (callback status 2 and 6). It never
	// changes the key, because ONLYOFFICE forbids a key change during a
	// force-save of an open document.
	SaveCount int `json:"save_count" gorm:"not null;default:0"`
	// Role is DocumentWorkspaceRoleTarget or DocumentWorkspaceRoleSource.
	Role string `json:"role" gorm:"type:varchar(16);not null;default:'target'"`
	// TextStatus is the state of a source's parsed text (see
	// DocumentSourceText*); "" for a target.
	TextStatus  string         `json:"text_status,omitempty" gorm:"type:varchar(16);not null;default:''"`
	LastSavedAt *time.Time     `json:"last_saved_at,omitempty"`
	ClosedAt    *time.Time     `json:"closed_at,omitempty"`
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`
	DeletedAt   gorm.DeletedAt `json:"-" gorm:"index"`
}

func (DocumentWorkspace) TableName() string { return "document_workspaces" }

func (w *DocumentWorkspace) BeforeCreate(_ *gorm.DB) error {
	if w.ID == "" {
		w.ID = uuid.NewString()
	}
	if w.Status == "" {
		w.Status = DocumentWorkspaceStatusOpen
	}
	if w.FileType == "" {
		w.FileType = "docx"
	}
	if w.Role == "" {
		w.Role = DocumentWorkspaceRoleTarget
	}
	return nil
}

// IsSource reports a source document (a chat upload, looked up only).
func (w *DocumentWorkspace) IsSource() bool {
	return w != nil && w.Role == DocumentWorkspaceRoleSource
}

// IsTarget reports a target document (an editor tab); a row without a role
// (written before roles existed) is one.
func (w *DocumentWorkspace) IsTarget() bool {
	return w != nil && w.Role != DocumentWorkspaceRoleSource
}

// DocumentWorkspaceText is the stored text of a source document: the parsed
// text and chunks copied from the chat upload (or, for a demoted target,
// the paragraphs of its last version). It lives as long as the workspace
// row, independent of the upload's TTL.
type DocumentWorkspaceText struct {
	WorkspaceID string `json:"workspace_id" gorm:"type:varchar(36);primaryKey"`
	TenantID    uint64 `json:"tenant_id" gorm:"not null;index"`
	Content     string `json:"-" gorm:"type:text"`
	// Chunks is a JSON array of TemporaryDocumentChunk; empty ("[]") when
	// the text was not chunked (a demoted target: one paragraph per line).
	Chunks     JSON      `json:"-" gorm:"type:jsonb;not null;default:'[]'"`
	TokenCount int       `json:"token_count" gorm:"not null;default:0"`
	ChunkCount int       `json:"chunk_count" gorm:"not null;default:0"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

func (DocumentWorkspaceText) TableName() string { return "document_workspace_texts" }

// Handle is the short name the agent uses for the document, e.g. "vb2".
func (w *DocumentWorkspace) Handle() string {
	return DocumentHandlePrefix + itoa(w.Position)
}

// Label names the document for the model: "vb2 · Tờ trình.docx".
func (w *DocumentWorkspace) Label() string {
	return w.Handle() + " · " + w.FileName
}

// EditorKey is the ONLYOFFICE document.key for the current revision. It must
// change only when the file content changes outside the editor; the server
// treats a reused key as "same cached document".
func (w *DocumentWorkspace) EditorKey() string {
	return w.ID + "-" + itoa(w.Revision)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}

// DocumentEditorConfig is what the frontend needs to mount the ONLYOFFICE
// editor: where the Document Server lives and the signed DocsAPI config.
type DocumentEditorConfig struct {
	DocumentServerURL string                 `json:"document_server_url"`
	Config            map[string]interface{} `json:"config"`
}

// DocumentWorkspaceView is the API representation of a workspace together
// with its editor config. Editor is nil when the editor feature is disabled.
type DocumentWorkspaceView struct {
	*DocumentWorkspace
	EditorKey string                `json:"editor_key"`
	Editor    *DocumentEditorConfig `json:"editor,omitempty"`
	// Handle is the agent's name for the document (vb1, vb2, …).
	Handle string `json:"handle"`
	// FormatCheck is the background NĐ30 format check started when the
	// document was opened; nil when none has run in this server process.
	FormatCheck *DocumentFormatCheck `json:"format_check,omitempty"`
	// Profile is the document's card (DocumentProfile.Public: no hash,
	// stale when edited since); nil before one was started.
	Profile *DocumentProfile `json:"profile,omitempty"`
}

// Background format check statuses. Queued: the document waits for one of
// the few check slots (each check makes two model calls, the second up to
// minutes long), StartedAt is then refreshed while it waits.
const (
	DocumentFormatCheckQueued  = "queued"
	DocumentFormatCheckRunning = "running"
	DocumentFormatCheckReady   = "ready"
	DocumentFormatCheckFailed  = "failed"
)

// DocumentFormatCheck reports the background format check of a workspace
// document, so the chat can tell the user the evaluation is ready to read.
type DocumentFormatCheck struct {
	Status string `json:"status"`
	// Revision is the workspace revision that was checked; a newer
	// revision makes the result stale.
	Revision int `json:"revision"`
	// DocumentType is the detected rule set slug, e.g. quy_che, and
	// DocumentTypeLabel its Vietnamese name, e.g. "Quy chế".
	DocumentType      string     `json:"document_type,omitempty"`
	DocumentTypeLabel string     `json:"document_type_label,omitempty"`
	StartedAt         time.Time  `json:"started_at"`
	FinishedAt        *time.Time `json:"finished_at,omitempty"`
	// CheckedSavedAt is the latest document save the result still
	// describes: the check's start, moved forward when a later save left
	// the format fingerprint unchanged (body wording only).
	CheckedSavedAt *time.Time `json:"checked_saved_at,omitempty"`
	// Fingerprint identifies the checked content's format (page setup,
	// paragraph formatting, the text at both ends).
	Fingerprint string `json:"fingerprint,omitempty"`
}

// DocumentFormatReport is the evaluation of a finished background format
// check, shown to the user as information (not as a chat answer).
type DocumentFormatReport struct {
	FileName          string                 `json:"file_name"`
	DocumentType      string                 `json:"document_type,omitempty"`
	DocumentTypeLabel string                 `json:"document_type_label,omitempty"`
	Summary           *DocumentFormatSummary `json:"summary,omitempty"`
	// Evaluation is the Markdown judgment against NĐ30/2020/NĐ-CP.
	Evaluation string    `json:"evaluation"`
	CheckedAt  time.Time `json:"checked_at"`
}

// DocumentFormatSummary counts the measured checks by status.
type DocumentFormatSummary struct {
	Pass int `json:"pass"`
	Fail int `json:"fail"`
	Warn int `json:"warn"`
	Skip int `json:"skip"`
}

// InProgress reports a check that has not finished: queued or running.
func (c *DocumentFormatCheck) InProgress() bool {
	return c != nil && (c.Status == DocumentFormatCheckQueued || c.Status == DocumentFormatCheckRunning)
}

// DocumentSelection is the text a user highlighted in the embedded editor and
// sent along with a chat turn, so the agent knows which passage to act on.
type DocumentSelection struct {
	Text string `json:"text"`
	// ParagraphHint is optional surrounding text the editor plugin could
	// capture (for example the whole paragraph), used to locate the passage
	// when Text alone is ambiguous.
	ParagraphHint string `json:"paragraph_hint,omitempty"`
	// DocumentID is the workspace (editor tab) the text was selected in;
	// empty from a client that predates multi-document sessions.
	DocumentID string `json:"document_id,omitempty"`
	// Document names that workspace for the model ("vb2 · Tờ trình.docx");
	// set by the server after it checked DocumentID, never by the client.
	Document string `json:"document,omitempty"`
}

// OnlyOfficeCallback is the JSON body the Document Server POSTs to the
// callback URL. Statuses: 1 editing, 2 ready for saving (final), 3 saving
// error, 4 closed with no changes, 6 force-saved while editing, 7 force-save
// error. ForceSaveType: 0 command service, 1 Save button, 2 timer, 3 form.
type OnlyOfficeCallback struct {
	Key           string                   `json:"key"`
	Status        int                      `json:"status"`
	URL           string                   `json:"url,omitempty"`
	ChangesURL    string                   `json:"changesurl,omitempty"`
	FileType      string                   `json:"filetype,omitempty"`
	ForceSaveType *int                     `json:"forcesavetype,omitempty"`
	Users         []string                 `json:"users,omitempty"`
	Actions       []OnlyOfficeCallbackUser `json:"actions,omitempty"`
	UserData      string                   `json:"userdata,omitempty"`
	Token         string                   `json:"token,omitempty"`
}

// OnlyOfficeCallbackUser is one entry of OnlyOfficeCallback.Actions
// (type 0 disconnect, 1 connect, 2 force-save request).
type OnlyOfficeCallbackUser struct {
	Type   int    `json:"type"`
	UserID string `json:"userid"`
}
