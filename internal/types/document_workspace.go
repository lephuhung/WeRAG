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

// DocumentWorkspace is the one editable .docx a chat session works on in the
// document-assistant agent. Only two files ever exist for it: the original
// upload (OriginalRef) and the latest version (CurrentRef). All AI edits are
// written into CurrentRef as Word tracked changes, so the file itself carries
// the history and the user reverts by rejecting a change in the editor.
type DocumentWorkspace struct {
	ID          string `json:"id" gorm:"type:varchar(36);primaryKey"`
	TenantID    uint64 `json:"tenant_id" gorm:"not null;index"`
	SessionID   string `json:"session_id" gorm:"type:varchar(36);not null;uniqueIndex"`
	UserID      string `json:"user_id" gorm:"type:varchar(36);not null;default:''"`
	OriginalRef string `json:"-" gorm:"type:text;not null"`
	CurrentRef  string `json:"-" gorm:"type:text;not null"`
	FileName    string `json:"file_name" gorm:"type:varchar(1024);not null"`
	// FileType is always "docx": a legacy .doc upload is converted when the
	// workspace is created so every tool works on OOXML.
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
	SaveCount   int            `json:"save_count" gorm:"not null;default:0"`
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
	return nil
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
}

// DocumentSelection is the text a user highlighted in the embedded editor and
// sent along with a chat turn, so the agent knows which passage to act on.
type DocumentSelection struct {
	Text string `json:"text"`
	// ParagraphHint is optional surrounding text the editor plugin could
	// capture (for example the whole paragraph), used to locate the passage
	// when Text alone is ambiguous.
	ParagraphHint string `json:"paragraph_hint,omitempty"`
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
