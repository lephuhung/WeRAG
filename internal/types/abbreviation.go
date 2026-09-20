package types

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// Abbreviation is one entry in the global (non-tenant-scoped) Vietnamese
// abbreviation dictionary, ported from AIRAG's `abbreviations` table.
//
// Rows are suggestions until an admin activates them (IsActive). Multiple
// active rows may share a short_form — the expander treats that case as
// ambiguous and reports the candidates instead of picking one silently.
type Abbreviation struct {
	ID          string         `json:"id"           gorm:"type:varchar(36);primaryKey"`
	ShortForm   string         `json:"short_form"   gorm:"type:varchar(50);not null"`
	FullForm    string         `json:"full_form"    gorm:"type:varchar(255);not null"`
	Description string         `json:"description"  gorm:"type:text;not null;default:''"`
	IsActive    bool           `json:"is_active"    gorm:"not null;default:false;index"`
	SuggestedBy string         `json:"suggested_by" gorm:"type:varchar(36);not null;default:''"`
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`
	DeletedAt   gorm.DeletedAt `json:"-"            gorm:"index"`
}

// TableName returns the table name for Abbreviation.
func (Abbreviation) TableName() string { return "abbreviations" }

// BeforeCreate assigns a UUID primary key when the caller did not.
func (m *Abbreviation) BeforeCreate(tx *gorm.DB) error {
	if m.ID == "" {
		m.ID = uuid.New().String()
	}
	return nil
}

// AbbreviationCreateRequest is the payload for POST /abbreviations.
type AbbreviationCreateRequest struct {
	ShortForm   string `json:"short_form"  binding:"required,max=50"`
	FullForm    string `json:"full_form"   binding:"required,max=255"`
	Description string `json:"description"`
}

// AbbreviationUpdateRequest is the payload for PATCH /abbreviations/:id.
// IsActive is a *bool so "absent" and "explicitly false" stay distinct —
// only admins may change it either way.
type AbbreviationUpdateRequest struct {
	ShortForm   *string `json:"short_form"  binding:"omitempty,max=50"`
	FullForm    *string `json:"full_form"   binding:"omitempty,max=255"`
	Description *string `json:"description"`
	IsActive    *bool   `json:"is_active"`
}
