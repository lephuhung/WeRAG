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
// B, from EffFrom (migration 000177; plan 2026-10-19 §11). Platform-wide.
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

// LegalProvisionLinkCandidate is a provision-level link a feed other than
// vbpl.vn pushed (POST /system/legal-documents/push), waiting for a
// SuperAdmin; approved, it is copied into legal_provision_links with
// Source = Feed (migration 000178, docs/legal-document-feed-spec.md §9).
type LegalProvisionLinkCandidate struct {
	ID                int64      `json:"id"                  gorm:"primaryKey;autoIncrement"`
	Feed              string     `json:"feed"                gorm:"type:varchar(32);not null;index:idx_legal_link_candidates_feed_doc,priority:1"`
	FeedDocID         string     `json:"feed_doc_id"         gorm:"type:varchar(64);not null;index:idx_legal_link_candidates_feed_doc,priority:2"`
	FeedURL           string     `json:"feed_url"            gorm:"type:text;not null;default:''"`
	TargetDocID       string     `json:"target_doc_id"       gorm:"type:varchar(64);not null;index:idx_legal_link_candidates_target,priority:1"`
	TargetElementID   string     `json:"target_element_id"   gorm:"type:varchar(160);not null;default:''"`
	TargetProvisionID string     `json:"target_provision_id" gorm:"type:varchar(64);not null;default:''"`
	TargetLevel       string     `json:"target_level"        gorm:"type:varchar(16);not null;default:''"`
	TargetPath        string     `json:"target_path"         gorm:"type:varchar(160);not null;default:''"`
	RelationCode      int        `json:"relation_code"       gorm:"type:smallint;not null"`
	Op                string     `json:"op"                  gorm:"type:varchar(32);not null"`
	AmenderDocNumber  string     `json:"amender_doc_number"  gorm:"type:varchar(128);not null;default:''"`
	AmenderIssueDate  string     `json:"amender_issue_date"  gorm:"type:varchar(16);not null;default:''"`
	AmenderIssuer     string     `json:"amender_issuer"      gorm:"type:varchar(255);not null;default:''"`
	AmenderDocID      string     `json:"amender_doc_id"      gorm:"type:varchar(64);not null;default:''"`
	SourceProvisionID string     `json:"source_provision_id" gorm:"type:varchar(64);not null;default:''"`
	SourcePath        string     `json:"source_path"         gorm:"type:varchar(160);not null;default:''"`
	EffFrom           *time.Time `json:"eff_from"            gorm:"type:date"`
	EffTo             *time.Time `json:"eff_to"              gorm:"type:date"`
	Note              string     `json:"note"                gorm:"type:text;not null;default:''"`
	MatchStatus       string     `json:"match_status"        gorm:"type:varchar(24);not null;index:idx_legal_link_candidates_review,priority:2"`
	MatchReason       string     `json:"match_reason"        gorm:"type:text;not null;default:''"`
	ReviewStatus      string     `json:"review_status"       gorm:"type:varchar(16);not null;default:'pending';index:idx_legal_link_candidates_target,priority:2;index:idx_legal_link_candidates_review,priority:1"`
	LinkID            *int64     `json:"link_id"`
	ReviewedBy        string     `json:"reviewed_by"         gorm:"type:varchar(64);not null;default:''"`
	ReviewedAt        *time.Time `json:"reviewed_at"`
	Detail            JSON       `json:"detail"              gorm:"type:jsonb"`
	CreatedAt         time.Time  `json:"created_at"`
	UpdatedAt         time.Time  `json:"updated_at"`
}

// TableName returns the table name for LegalProvisionLinkCandidate.
func (LegalProvisionLinkCandidate) TableName() string { return "legal_provision_link_candidates" }

// Match states of a pushed link (LegalProvisionLinkCandidate.MatchStatus).
const (
	// LegalLinkMatched: A, B and both provisions were found.
	LegalLinkMatched = "matched"
	// LegalLinkDocOnly: B was found, its provision was not (no or unknown path).
	LegalLinkDocOnly = "doc_only"
	// LegalLinkTargetMissing: A's copy in the knowledge base has no provision at the path.
	LegalLinkTargetMissing = "target_missing"
	// LegalLinkUnresolved: B could not be told apart in the catalogue.
	LegalLinkUnresolved = "unresolved"
	// LegalLinkDuplicate: legal_provision_links already holds the link.
	LegalLinkDuplicate = "duplicate"
)

// Review states of a pushed link (LegalProvisionLinkCandidate.ReviewStatus).
const (
	// LegalLinkAutoReviewer is the reviewed_by of a matched candidate approved
	// without a person (service.LegalFeedAutoApproveSetting).
	LegalLinkAutoReviewer = "auto:matched"

	LegalLinkReviewPending  = "pending"
	LegalLinkReviewApproved = "approved"
	LegalLinkReviewRejected = "rejected"
	// LegalLinkReviewAuto: a duplicate of a link already held; nothing to review.
	LegalLinkReviewAuto = "auto"
)
