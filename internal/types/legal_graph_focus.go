package types

import "errors"

// ErrLegalGraphFocusNotFound: the document is not reachable through the
// knowledge base (neither held nor a direct neighbour of a held document).
var ErrLegalGraphFocusNotFound = errors.New("legal document not reachable from this knowledge base")

// LegalGraphDoc is one catalogue document of the focused legal graph of a
// knowledge base: the focus, a neighbour or the other side of a provision
// row.
type LegalGraphDoc struct {
	ID             string `json:"id"`
	DocumentNumber string `json:"document_number"`
	DocType        string `json:"doc_type"`
	Title          string `json:"title"`
	Status         string `json:"status"`
	Issuer         string `json:"issuer"`
	IssuedOn       string `json:"issued_on"`    // YYYY-MM-DD or ""
	EffectiveOn    string `json:"effective_on"` // YYYY-MM-DD or ""
	// InKB: the knowledge base holds the document (approved legal link).
	InKB           bool   `json:"in_kb"`
	KnowledgeID    string `json:"knowledge_id,omitempty"`
	KnowledgeTitle string `json:"knowledge_title,omitempty"`
	// Focusable: the document may be focused through this knowledge base
	// (it is held, or a direct neighbour of a held document).
	Focusable bool `json:"focusable"`
	// Degree counts the document-level relations of a held document
	// (candidates only; "căn cứ" not counted).
	Degree int `json:"degree,omitempty"`
}

// Directions of a LegalGraphNeighbour.
const (
	// LegalGraphOut: the focus acts on the neighbour (amends, replaces,
	// repeals, guides it, or is based on it).
	LegalGraphOut = "out"
	// LegalGraphIn: the neighbour acts on the focus.
	LegalGraphIn = "in"
)

// LegalGraphNeighbour is a document related to the focus, one per
// document and direction.
type LegalGraphNeighbour struct {
	Doc       *LegalGraphDoc `json:"doc"`
	Direction string         `json:"direction"`
	// Relations are the catalogue relations between the two, strongest
	// first (Relations[0] draws the edge).
	Relations []string `json:"relations"`
	// FromProvisions: no catalogue relation joins the two; the relation is
	// read from the provision links (legal_provision_links).
	FromProvisions bool `json:"from_provisions,omitempty"`
}

// LegalGraphProvisionRow is one provision-level link touching the focus.
// In ProvisionsIn the focus is the target (DocID acts on it); in
// ProvisionsOut the focus is the source (it acts on DocID).
type LegalGraphProvisionRow struct {
	ID int64 `json:"id"`
	// TargetPath / TargetLevel: the provision acted on ("dieu:5/khoan:2/diem:a";
	// article / clause / item), in the target document.
	TargetPath  string `json:"target_path"`
	TargetLevel string `json:"target_level"`
	// Op: sua_doi_bo_sung, bai_bo, bo_sung, thay_the, giai_thich,
	// dinh_chinh, tam_ngung, dinh_chi.
	Op string `json:"op"`
	// SourcePath: the provision of the acting document ("" when unknown).
	SourcePath string `json:"source_path"`
	// DocID: the other document (Documents holds it); "" when unknown.
	DocID   string `json:"doc_id"`
	EffFrom string `json:"eff_from"` // YYYY-MM-DD or ""
	EffTo   string `json:"eff_to"`
}

// LegalGraphFocus is the ego graph of one catalogue document seen from a
// knowledge base, with its provision-level links in both directions.
type LegalGraphFocus struct {
	// Focus is nil when the knowledge base holds no catalogued document.
	Focus      *LegalGraphDoc         `json:"focus"`
	Neighbours []*LegalGraphNeighbour `json:"neighbours"`
	// NeighbourTotals counts neighbours per "<direction>:<relation>" before
	// the per-group cap; NeighboursTruncated is set when some were dropped.
	NeighbourTotals     map[string]int `json:"neighbour_totals"`
	NeighboursTruncated bool           `json:"neighbours_truncated"`

	ProvisionsIn        []*LegalGraphProvisionRow `json:"provisions_in"`
	ProvisionsOut       []*LegalGraphProvisionRow `json:"provisions_out"`
	ProvisionsInTotal   int64                     `json:"provisions_in_total"`
	ProvisionsOutTotal  int64                     `json:"provisions_out_total"`
	ProvisionsTruncated bool                      `json:"provisions_truncated"`
	// Documents resolves the provision rows' DocID.
	Documents map[string]*LegalGraphDoc `json:"documents"`

	// Candidates are held documents for the picker: those matching q, or
	// the best connected when q is empty.
	Candidates []*LegalGraphDoc `json:"candidates"`
	// AnchorCount is how many catalogued documents the KB holds.
	AnchorCount int `json:"anchor_count"`
}
