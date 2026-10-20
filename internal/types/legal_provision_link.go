package types

import "time"

// LegalProvisionLinkSourceEditor marks edges tagged by vbpl.vn editors.
const LegalProvisionLinkSourceEditor = "vbpl_editor"

// Where a LegalProvisionLink's document B and date come from.
const (
	LegalLinkFromHTML            = "html"             // B's crawled page holds the provision id
	LegalLinkFromProvisionTarget = "provision_target" // vbpl's provision-target answer
	LegalLinkFromCatalogue       = "catalogue"        // B's catalogue effective_on
)

// LegalProvisionLink is one provision-level relation a vbpl.vn editor
// tagged on the amended document A: provision TargetProvisionID of A is
// acted on (RelationCode / Op) by provision SourceProvisionID of document
// B, from EffFrom (migration 000176; plan 2026-10-19 §11). Platform-wide.
type LegalProvisionLink struct {
	ID                int64      `json:"id"                  gorm:"primaryKey;autoIncrement"`
	TargetDocID       string     `json:"target_doc_id"       gorm:"type:varchar(64);not null;index:idx_legal_provision_links_target,priority:1"`
	TargetProvisionID string     `json:"target_provision_id" gorm:"type:varchar(64);not null;uniqueIndex:ux_legal_provision_links_edge,priority:1"`
	TargetLevel       string     `json:"target_level"        gorm:"type:varchar(16);not null;default:''"`
	TargetPath        string     `json:"target_path"         gorm:"type:varchar(160);not null;default:'';index:idx_legal_provision_links_target,priority:2"`
	RelationCode      int        `json:"relation_code"       gorm:"type:smallint;not null;uniqueIndex:ux_legal_provision_links_edge,priority:2"`
	Op                string     `json:"op"                  gorm:"type:varchar(32);not null"`
	TagAttr           string     `json:"tag_attr"            gorm:"type:varchar(16);not null;default:'type'"`
	SourceDocID       string     `json:"source_doc_id"       gorm:"type:varchar(64);not null;default:'';index:idx_legal_provision_links_source_doc"`
	SourceDocFrom     string     `json:"source_doc_from"     gorm:"type:varchar(32);not null;default:''"`
	SourceProvisionID string     `json:"source_provision_id" gorm:"type:varchar(64);not null;uniqueIndex:ux_legal_provision_links_edge,priority:3;index:idx_legal_provision_links_source_prov"`
	SourcePath        string     `json:"source_path"         gorm:"type:varchar(160);not null;default:''"`
	EffFrom           *time.Time `json:"eff_from"            gorm:"type:date"`
	EffTo             *time.Time `json:"eff_to"              gorm:"type:date"`
	EffFromSource     string     `json:"eff_from_source"     gorm:"type:varchar(32);not null;default:''"`
	Source            string     `json:"source"              gorm:"type:varchar(32);not null;default:'vbpl_editor'"`
	CreatedAt         time.Time  `json:"created_at"`
	UpdatedAt         time.Time  `json:"updated_at"`
}

// TableName returns the table name for LegalProvisionLink.
func (LegalProvisionLink) TableName() string { return "legal_provision_links" }
