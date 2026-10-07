package types

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// Document revision sources: what produced a snapshot on the workspace
// timeline.
const (
	DocumentRevisionSourceAI      = "ai"
	DocumentRevisionSourceManual  = "manual"
	DocumentRevisionSourceClose   = "close"
	DocumentRevisionSourceRestore = "restore"
)

// IsValidDocumentRevisionSource reports whether s is one of the sources above.
func IsValidDocumentRevisionSource(s string) bool {
	switch s {
	case DocumentRevisionSourceAI, DocumentRevisionSourceManual,
		DocumentRevisionSourceClose, DocumentRevisionSourceRestore:
		return true
	}
	return false
}

// DocumentRevision is one point on a document workspace's snapshot timeline:
// a stored version of the file (Ref) the user can restore. Seq numbers the
// snapshots of one workspace, starting at 1.
type DocumentRevision struct {
	ID          string    `json:"id" gorm:"type:varchar(36);primaryKey"`
	WorkspaceID string    `json:"workspace_id" gorm:"type:varchar(36);not null;index"`
	TenantID    uint64    `json:"tenant_id" gorm:"not null"`
	Seq         int       `json:"seq" gorm:"not null"`
	Ref         string    `json:"-" gorm:"type:text;not null"`
	Label       string    `json:"label" gorm:"type:varchar(512);not null;default:''"`
	Source      string    `json:"source" gorm:"type:varchar(16);not null"`
	FileSize    int64     `json:"file_size" gorm:"not null;default:0"`
	CreatedAt   time.Time `json:"created_at"`
}

func (DocumentRevision) TableName() string { return "document_revisions" }

func (r *DocumentRevision) BeforeCreate(_ *gorm.DB) error {
	if r.ID == "" {
		r.ID = uuid.NewString()
	}
	return nil
}
