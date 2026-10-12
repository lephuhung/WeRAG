package interfaces

import (
	"context"

	"github.com/Tencent/WeKnora/internal/types"
)

// LegalCatalogRepository persists the platform-wide legal catalogue.
type LegalCatalogRepository interface {
	// UpsertDocuments inserts or refreshes documents by ID.
	UpsertDocuments(ctx context.Context, docs []*types.LegalDocument) error
	// ReplaceRelations swaps the whole relation table for rels in one
	// transaction (a catalogue import is always a full snapshot).
	ReplaceRelations(ctx context.Context, rels []*types.LegalDocumentRelation) error

	// FindByNumberKey returns documents whose number key equals key, or —
	// when none does — starts with key + "/" (a number typed without its
	// suffix, e.g. "30/2020"). Newest first.
	FindByNumberKey(ctx context.Context, key string, limit int) ([]*types.LegalDocument, error)
	// SearchByTitle returns documents whose title contains every word.
	// Newest first.
	SearchByTitle(ctx context.Context, words []string, limit int) ([]*types.LegalDocument, error)
	// ListLinks returns the relations of a document with the other side
	// loaded, limited to the given relations per direction.
	ListLinks(ctx context.Context, id string, outgoing, incoming []types.LegalRelation) ([]types.LegalRelationLink, error)
	// ListEdges returns the relations of the given kinds that touch ids:
	// with incoming, edges ending at an id (another document acts on it),
	// otherwise edges starting from one. One hop of a graph walk.
	ListEdges(ctx context.Context, ids []string, rels []types.LegalRelation, incoming bool) ([]types.LegalDocumentRelation, error)

	// GetByIDs returns the documents with the given IDs (any order).
	GetByIDs(ctx context.Context, ids []string) ([]*types.LegalDocument, error)
	// SaveDocument inserts or fully overwrites one row.
	SaveDocument(ctx context.Context, doc *types.LegalDocument) error
	// SetSourcePending records (or clears, with nil) portal values that
	// differ from a reviewed row.
	SetSourcePending(ctx context.Context, id string, pending types.JSON) error
}

// LegalPublicKnowledge is a knowledge of a platform public KB.
type LegalPublicKnowledge struct {
	KnowledgeID     string
	KnowledgeBaseID string
	// ExternalID is the source's own ID when the document was imported with
	// one (knowledges.metadata.external_id, e.g. the vbpl.vn ID).
	ExternalID string
}

// LegalReviewRepository persists the review of legal identities of
// documents in platform public KBs.
type LegalReviewRepository interface {
	// UnlinkedPublicKnowledge lists parsed knowledge of platform public KBs
	// that has no link row yet.
	UnlinkedPublicKnowledge(ctx context.Context, limit int) ([]LegalPublicKnowledge, error)
	// HeaderText returns the start of a knowledge's text (its first text
	// chunks in order), at most maxChars bytes.
	HeaderText(ctx context.Context, knowledgeID string, maxChars int) (string, error)
	// SaveLink inserts or overwrites a link.
	SaveLink(ctx context.Context, link *types.LegalDocumentLink) error
	// GetLink returns one link, or nil when absent.
	GetLink(ctx context.Context, knowledgeID string) (*types.LegalDocumentLink, error)
	// ListLinks pages links of live knowledge, filtered by status (empty =
	// all) and KB (empty = every KB), newest first, with each knowledge's
	// title and KB name.
	ListLinks(ctx context.Context, status types.LegalReviewStatus, kbID string, offset, limit int) ([]LegalLinkRow, int64, error)
	// ListSupersededLinks pages approved links whose document was
	// replaced, repealed or ended by another catalogue document, or whose
	// status says it expired. kbID "" = every KB.
	ListSupersededLinks(ctx context.Context, kbID string, offset, limit int) ([]LegalLinkRow, int64, error)
	// ApprovedDocumentIDs returns which of ids have an approved link.
	ApprovedDocumentIDs(ctx context.Context, ids []string) (map[string]bool, error)
	// ApprovedLinksForKnowledge returns approved links of the given
	// knowledge IDs keyed by knowledge ID.
	ApprovedLinksForKnowledge(ctx context.Context, knowledgeIDs []string) (map[string]*types.LegalDocumentLink, error)
	// KnowledgeForDocuments returns the live public-KB copies with an
	// approved link of the given catalogue documents, keyed by document ID.
	KnowledgeForDocuments(ctx context.Context, documentIDs []string) (map[string][]types.LegalKnowledgeRef, error)
}

// LegalLinkRow is a link with the knowledge title and KB name.
type LegalLinkRow struct {
	Link              *types.LegalDocumentLink
	KnowledgeTitle    string
	KnowledgeBaseName string
}

// LegalCatalogService answers validity questions from the legal catalogue.
type LegalCatalogService interface {
	// Lookup finds documents by số ký hiệu (preferred) or title words and
	// returns each with the relations that bear on its validity.
	Lookup(ctx context.Context, documentNumber, title string, limit int) ([]*types.LegalStatusReport, error)
	// Related finds documents the same way as Lookup and walks the relation
	// graph from each, up to depth hops, marking the related documents a
	// public KB holds. Fully expired documents are skipped unless
	// includeExpired (history walks always keep them).
	Related(ctx context.Context, documentNumber, title string, mode types.LegalRelatedMode,
		depth int, includeExpired bool) ([]*types.LegalRelatedReport, error)
	// ExpandRelated returns the documents guiding (two hops) or amending
	// (one hop) the catalogue documents of the given knowledge, those with
	// an approved legal link, in the order of knowledgeIDs. Expired
	// documents are included: the caller decides what to show of them.
	ExpandRelated(ctx context.Context, knowledgeIDs []string) ([]types.LegalExpansion, error)
	// KBGraph returns the relation graph of a KB's catalogued documents
	// (approved legal links) and their direct neighbours, at most limit nodes.
	KBGraph(ctx context.Context, kbID string, limit int) (*types.GraphView, error)
}

// LegalReviewService drives the SuperAdmin review of legal identities.
type LegalReviewService interface {
	// Scan creates pending suggestions for public-KB knowledge without a
	// link; returns how many were created.
	Scan(ctx context.Context) (int, error)
	// List pages the review queue. status "" lists all; "alerts" lists
	// approved documents superseded by a newer one. kbID "" = every KB.
	List(ctx context.Context, status, kbID string, offset, limit int) ([]*types.LegalReviewItem, int64, error)
	// Decide applies a SuperAdmin decision to one knowledge.
	Decide(ctx context.Context, knowledgeID string, d *types.LegalReviewDecision) (*types.LegalReviewItem, error)
	// SearchCatalog finds catalogue rows by number or title words, for the
	// reviewer to pick a match by hand.
	SearchCatalog(ctx context.Context, query string, limit int) ([]*types.LegalDocument, error)
	// AnnotateSearchResults prepends the reviewed legal identity and
	// validity of each result's document to KnowledgeCustomMetadata and
	// sets LegalSuperseded. Best effort: failures are logged, not returned.
	AnnotateSearchResults(ctx context.Context, results []*types.SearchResult)
	// Spellcheck proofreads chunk texts of a reviewed knowledge with its
	// KB's summary model. Nothing is saved: the reviewer applies fixes.
	Spellcheck(ctx context.Context, knowledgeID string, items []types.LegalSpellcheckItem) ([]types.LegalSpellcheckFinding, error)
}
