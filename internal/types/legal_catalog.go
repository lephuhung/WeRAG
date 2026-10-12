package types

import "time"

// LegalDocument is one row of the platform-wide legal catalogue: the
// metadata of a Vietnamese legal document (no full text), imported from a
// vbpl.vn snapshot. Not tenant-scoped — public law is the same for everyone.
//
// Status is the portal's wording at SourceUpdatedAt ("Còn hiệu lực",
// "Hết hiệu lực toàn bộ", …); it is a snapshot, not a live mirror.
type LegalDocument struct {
	ID              string     `json:"id"                gorm:"type:varchar(64);primaryKey"`
	Source          string     `json:"source"            gorm:"type:varchar(32);not null;default:'vbpl'"`
	DocumentNumber  string     `json:"document_number"   gorm:"type:varchar(128);not null;default:''"`
	NumberKey       string     `json:"-"                 gorm:"type:varchar(128);not null;default:'';index"`
	Title           string     `json:"title"             gorm:"type:text;not null;default:''"`
	DocType         string     `json:"doc_type"          gorm:"type:varchar(64);not null;default:''"`
	Issuer          string     `json:"issuer"            gorm:"type:varchar(255);not null;default:''"`
	Signer          string     `json:"signer"            gorm:"type:varchar(255);not null;default:''"`
	SignerTitle     string     `json:"signer_title"      gorm:"type:varchar(255);not null;default:''"`
	Field           string     `json:"field"             gorm:"type:text;not null;default:''"`
	IssuedOn        *time.Time `json:"issued_on"         gorm:"type:date"`
	EffectiveOn     *time.Time `json:"effective_on"      gorm:"type:date"`
	ExpiredOn       *time.Time `json:"expired_on"        gorm:"type:date"`
	Status          string     `json:"status"            gorm:"type:varchar(64);not null;default:''"`
	SourceUpdatedAt *time.Time `json:"source_updated_at"`
	// ReviewedAt is set once a SuperAdmin confirmed the row: from then on
	// it is the curated truth and a re-import parks differing portal
	// values in SourcePending instead of overwriting.
	ReviewedBy    string     `json:"reviewed_by"    gorm:"type:varchar(36);not null;default:''"`
	ReviewedAt    *time.Time `json:"reviewed_at"`
	SourcePending JSON       `json:"source_pending" gorm:"type:jsonb"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
}

// LegalSupersededScoreFactor scales the rerank score of a search result
// whose reviewed legal document was replaced or ended: it stays findable
// (questions about the old rule) but ranks after in-force law.
const LegalSupersededScoreFactor = 0.75

// LegalDocumentSourceManual marks catalogue rows a reviewer added by hand.
const LegalDocumentSourceManual = "manual"

// LegalDocumentSourceVBPL marks catalogue rows imported from vbpl.vn.
const LegalDocumentSourceVBPL = "vbpl"

// LegalReviewerVBPL is the reviewed_by of links approved without a reviewer:
// the knowledge was imported from vbpl.vn and names its catalogue row.
const LegalReviewerVBPL = "vbpl-import"

// TableName returns the table name for LegalDocument.
func (LegalDocument) TableName() string { return "legal_documents" }

// LegalRelation names what a document does to another one. Edges are stored
// in one direction only: FromID <relation> ToID (e.g. FromID replaces ToID).
type LegalRelation string

const (
	LegalRelationReplaces         LegalRelation = "replaces"
	LegalRelationRepeals          LegalRelation = "repeals"
	LegalRelationExpires          LegalRelation = "expires"
	LegalRelationPartiallyExpires LegalRelation = "partially_expires"
	LegalRelationSuspends         LegalRelation = "suspends"
	LegalRelationAmends           LegalRelation = "amends"
	LegalRelationGuides           LegalRelation = "guides"
	LegalRelationInterprets       LegalRelation = "interprets"
	LegalRelationConsolidates     LegalRelation = "consolidates"
	LegalRelationCorrects         LegalRelation = "corrects"
	LegalRelationBasedOn          LegalRelation = "based_on"
	LegalRelationReferences       LegalRelation = "references"
	LegalRelationRelated          LegalRelation = "related"
)

// LegalDocumentRelation is one directed edge of the legal catalogue.
type LegalDocumentRelation struct {
	FromID      string        `json:"from_id"      gorm:"type:varchar(64);primaryKey"`
	ToID        string        `json:"to_id"        gorm:"type:varchar(64);primaryKey;index"`
	Relation    LegalRelation `json:"relation"     gorm:"type:varchar(32);primaryKey"`
	SourceLabel string        `json:"source_label" gorm:"type:varchar(128);not null;default:''"`
	CreatedAt   time.Time     `json:"created_at"`
}

// TableName returns the table name for LegalDocumentRelation.
func (LegalDocumentRelation) TableName() string { return "legal_document_relations" }

// LegalRelationLink is a relation seen from one catalogue document: the
// other document and whether it acts on this one (Incoming) or this one
// acts on it.
type LegalRelationLink struct {
	Relation LegalRelation  `json:"relation"`
	Incoming bool           `json:"incoming"`
	Other    *LegalDocument `json:"other"`
}

// LegalStatusReport is the catalogue's answer for one document.
type LegalStatusReport struct {
	Document *LegalDocument      `json:"document"`
	Links    []LegalRelationLink `json:"links"`
}

// LegalRelatedMode names a walk over the relation graph from one document.
type LegalRelatedMode string

const (
	// LegalRelatedGuidance follows "is guided by" down: Luật → Nghị định →
	// Thông tư that detail or guide it.
	LegalRelatedGuidance LegalRelatedMode = "guidance"
	// LegalRelatedAmendments lists what amends, corrects or consolidates it.
	LegalRelatedAmendments LegalRelatedMode = "amendments"
	// LegalRelatedHistory follows replacement and repeal both ways: the
	// versions before and after it.
	LegalRelatedHistory LegalRelatedMode = "history"
	// LegalRelatedBasis follows "based on" and "guides" up to the law it
	// implements.
	LegalRelatedBasis LegalRelatedMode = "basis"
)

// LegalKnowledgeRef is a copy of a catalogue document in a public KB, with
// an approved legal link.
type LegalKnowledgeRef struct {
	KnowledgeID       string `json:"knowledge_id"`
	KnowledgeBaseID   string `json:"knowledge_base_id"`
	KnowledgeBaseName string `json:"knowledge_base_name"`
	Title             string `json:"title"`
}

// LegalRelatedDocument is a document reached by a relation walk: Relation
// and Incoming describe the edge from ParentID (Incoming = this document
// acts on the parent).
type LegalRelatedDocument struct {
	Document  *LegalDocument      `json:"document"`
	Depth     int                 `json:"depth"`
	ParentID  string              `json:"parent_id"`
	Relation  LegalRelation       `json:"relation"`
	Incoming  bool                `json:"incoming"`
	Knowledge []LegalKnowledgeRef `json:"knowledge,omitempty"`
}

// LegalExpansion is a document related to a search hit through the relation
// graph: Anchor is the hit's catalogue document, AnchorKnowledgeID the hit.
type LegalExpansion struct {
	Anchor            *LegalDocument       `json:"anchor"`
	AnchorKnowledgeID string               `json:"anchor_knowledge_id"`
	Related           LegalRelatedDocument `json:"related"`
}

// LegalRelatedReport is the result of one relation walk.
type LegalRelatedReport struct {
	Document *LegalDocument         `json:"document"`
	Mode     LegalRelatedMode       `json:"mode"`
	Related  []LegalRelatedDocument `json:"related"`
	// Truncated reports that a level held more documents than the walk keeps.
	Truncated bool `json:"truncated"`
}

// LegalReviewStatus is the review state of a knowledge's legal identity.
type LegalReviewStatus string

const (
	// LegalReviewPending: suggested by the parser, not yet confirmed.
	LegalReviewPending LegalReviewStatus = "pending"
	// LegalReviewApproved: a SuperAdmin confirmed the linked catalogue row.
	LegalReviewApproved LegalReviewStatus = "approved"
	// LegalReviewRejected: not a legal document (or not worth tracking).
	LegalReviewRejected LegalReviewStatus = "rejected"
)

// LegalDocumentLink ties a knowledge of a platform public KB to its
// catalogue row, with the parser's suggestion and the review state.
type LegalDocumentLink struct {
	KnowledgeID     string            `json:"knowledge_id"      gorm:"type:varchar(36);primaryKey"`
	KnowledgeBaseID string            `json:"knowledge_base_id" gorm:"type:varchar(36);not null"`
	LegalDocumentID string            `json:"legal_document_id" gorm:"type:varchar(64);not null;default:'';index"`
	ReviewStatus    LegalReviewStatus `json:"review_status"     gorm:"type:varchar(16);not null;default:'pending';index"`
	Suggestion      JSON              `json:"suggestion"        gorm:"type:jsonb"`
	ReviewedBy      string            `json:"reviewed_by"       gorm:"type:varchar(36);not null;default:''"`
	ReviewedAt      *time.Time        `json:"reviewed_at"`
	CreatedAt       time.Time         `json:"created_at"`
	UpdatedAt       time.Time         `json:"updated_at"`
}

// TableName returns the table name for LegalDocumentLink.
func (LegalDocumentLink) TableName() string { return "legal_document_links" }

// LegalIdentitySuggestion is what the header parse proposes for a
// knowledge, stored on its link for the reviewer.
type LegalIdentitySuggestion struct {
	DocumentNumber string `json:"document_number,omitempty"`
	DocType        string `json:"doc_type,omitempty"`
	Issuer         string `json:"issuer,omitempty"`
	IssuedOn       string `json:"issued_on,omitempty"` // dd/mm/yyyy as printed
	// CatalogMatches are catalogue rows whose number key equals the parsed
	// số hiệu (several when the number is shared).
	CatalogMatches []string `json:"catalog_matches,omitempty"`
}

// LegalReviewItem is one row of the SuperAdmin review queue.
type LegalReviewItem struct {
	Link              *LegalDocumentLink       `json:"link"`
	KnowledgeTitle    string                   `json:"knowledge_title"`
	KnowledgeBaseName string                   `json:"knowledge_base_name"`
	Suggestion        *LegalIdentitySuggestion `json:"suggestion,omitempty"`
	Document          *LegalDocument           `json:"document,omitempty"`
	Candidates        []*LegalDocument         `json:"candidates,omitempty"`
	// SupersededBy lists catalogue documents that replace, repeal or end
	// the linked one; InKB tells whether each is itself in a public KB.
	SupersededBy []LegalSupersession `json:"superseded_by,omitempty"`
}

// LegalSupersession is a newer document acting on a linked one.
type LegalSupersession struct {
	Relation LegalRelation  `json:"relation"`
	Document *LegalDocument `json:"document"`
	InKB     bool           `json:"in_kb"`
}

// LegalReviewDecision is the SuperAdmin's answer for one knowledge.
type LegalReviewDecision struct {
	// Action is "approve" or "reject".
	Action string `json:"action"`
	// LegalDocumentID picks an existing catalogue row; empty with a
	// Document creates a manual row.
	LegalDocumentID string `json:"legal_document_id"`
	// Document carries the reviewed fields written onto the row.
	Document *LegalDocumentFields `json:"document"`
}

// LegalDocumentFields are the reviewer-editable fields of a catalogue row.
// Dates are YYYY-MM-DD; empty clears.
type LegalDocumentFields struct {
	DocumentNumber string `json:"document_number"`
	Title          string `json:"title"`
	DocType        string `json:"doc_type"`
	Issuer         string `json:"issuer"`
	Signer         string `json:"signer"`
	Field          string `json:"field"`
	IssuedOn       string `json:"issued_on"`
	EffectiveOn    string `json:"effective_on"`
	ExpiredOn      string `json:"expired_on"`
	Status         string `json:"status"`
}

// LegalSpellcheckItem is one chunk text sent for proofreading — the text as
// the reviewer currently has it, saved or not.
type LegalSpellcheckItem struct {
	ChunkID string `json:"chunk_id"`
	Content string `json:"content"`
}

// LegalSpellcheckFinding is one spelling error in a chunk; Wrong is copied
// verbatim from the submitted content.
type LegalSpellcheckFinding struct {
	ChunkID string `json:"chunk_id"`
	Wrong   string `json:"wrong"`
	Correct string `json:"correct"`
	Reason  string `json:"reason"`
}
