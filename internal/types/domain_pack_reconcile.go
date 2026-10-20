package types

import "time"

// DomainPackRequestOriginPrefix marks a legal document request recorded by
// the reconcile of a domain pack's catalogue: Detail["origin"] is
// "pack:<name>". Source keeps the chat gate's meaning ("catalogue" when the
// catalogue knows the document, "" when it does not), so the origin lives
// in Detail and the pack name has no column length limit.
const DomainPackRequestOriginPrefix = "pack:"

// DomainPackRequestDetailOrigin is the Detail key holding the origin.
const DomainPackRequestDetailOrigin = "origin"

// DomainPackReconcileResult is what POST /system/domain-packs/:name/reconcile
// returns: every number of the pack's catalogue.yaml checked against the
// legal catalogue (legal_documents), which it never changes.
// docs/domain-pack-standard.md §4 layer b.
type DomainPackReconcileResult struct {
	Pack    string `json:"pack"`
	Version string `json:"version"`
	// Found: exactly one catalogue row has the number.
	Found []DomainPackReconcileFound `json:"found"`
	// Missing: no catalogue row has it; a legal document request was
	// recorded (or an existing one is reported).
	Missing []DomainPackReconcileMissing `json:"missing"`
	// Ambiguous: several catalogue rows share the number (số hiệu is not
	// unique); the SuperAdmin decides which one the pack means.
	Ambiguous []DomainPackReconcileAmbiguous `json:"ambiguous"`
	// Errors lists the numbers whose check failed; the others are still
	// reported, so one bad row does not hide the rest.
	Errors []DomainPackReconcileError `json:"errors"`
}

// DomainPackReconcileError is one number whose check failed.
type DomainPackReconcileError struct {
	Number string `json:"number"`
	Error  string `json:"error"`
}

// DomainPackReconcileRequest is the legal document request the reconcile
// found or recorded for a document no knowledge base holds.
type DomainPackReconcileRequest struct {
	ID     string `json:"id"`
	Status string `json:"status"`
	// Created is true when this reconcile recorded it, false when an
	// existing one (any status) was found and left as it was.
	Created bool `json:"created"`
}

// DomainPackCatalogueRef is the pack's own entry for a number.
type DomainPackCatalogueRef struct {
	Number  string `json:"number"`
	Title   string `json:"title,omitempty"`
	UsedFor string `json:"used_for"`
}

// DomainPackReconcileDocument is one legal_documents row as the reconcile
// reports it.
type DomainPackReconcileDocument struct {
	ID             string `json:"id"`
	DocumentNumber string `json:"document_number"`
	Title          string `json:"title"`
	DocType        string `json:"doc_type"`
	Issuer         string `json:"issuer"`
	Status         string `json:"status"`
	EffectiveOn    string `json:"effective_on,omitempty"` // YYYY-MM-DD
	ExpiredOn      string `json:"expired_on,omitempty"`   // YYYY-MM-DD
	// StatusNote is set when the catalogue holds the document expired,
	// replaced, suspended, not yet in force or amended: what the pack says
	// about it needs checking.
	StatusNote string `json:"status_note,omitempty"`
	// HeldInKB: a public knowledge base holds the full text (an approved
	// legal identity link, or an ingested copy not linked yet).
	HeldInKB bool `json:"held_in_kb"`
	// Request is the request to add the full text when no KB holds it.
	Request *DomainPackReconcileRequest `json:"request,omitempty"`
}

// DomainPackReconcileFound is a number with exactly one catalogue row.
type DomainPackReconcileFound struct {
	DomainPackCatalogueRef
	Document DomainPackReconcileDocument `json:"document"`
}

// DomainPackReconcileAmbiguous is a number with several catalogue rows.
type DomainPackReconcileAmbiguous struct {
	DomainPackCatalogueRef
	Documents []DomainPackReconcileDocument `json:"documents"`
}

// DomainPackReconcileMissing is a number the catalogue does not hold.
type DomainPackReconcileMissing struct {
	DomainPackCatalogueRef
	Request *DomainPackReconcileRequest `json:"request,omitempty"`
}

// DomainPackSummary is one shipped pack in the SuperAdmin list
// (GET /system/domain-packs).
type DomainPackSummary struct {
	Name          string   `json:"name"`
	Description   string   `json:"description"`
	Version       string   `json:"version"`
	AsOf          string   `json:"as_of"`     // YYYY-MM-DD
	ReviewBy      string   `json:"review_by"` // YYYY-MM-DD
	Overdue       bool     `json:"overdue"`
	Domain        string   `json:"domain"`
	Licence       string   `json:"licence"`
	Attribution   string   `json:"attribution"`
	Origin        string   `json:"origin"`
	RequiredTools []string `json:"required_tools"`
	DocumentCount int      `json:"document_count"`
	FormulaCount  int      `json:"formula_count"`
	AgentCount    int      `json:"agent_count"`
}

// DomainPackDraft is a draft the pack mentions; Date is "" when the source
// does not know which draft it is.
type DomainPackDraft struct {
	Title   string `json:"title"`
	Version string `json:"version"`
	Date    string `json:"date,omitempty"` // YYYY-MM-DD
}

// DomainPackDetail is GET /system/domain-packs/:name: the pack's cited
// documents with a read-only inspection against the legal catalogue, its
// formulas with the state of their basis, and the agents using it. Nothing
// is written; POST …/reconcile is the call that records requests.
type DomainPackDetail struct {
	DomainPackSummary
	AnswerFooter   string                     `json:"answer_footer"`
	DraftDocuments []DomainPackDraft          `json:"draft_documents"`
	Documents      []DomainPackDocumentView   `json:"documents"`
	Formulas       []DomainPackFormulaView    `json:"formulas"`
	Agents         []DomainPackAgent          `json:"agents"`
	Errors         []DomainPackReconcileError `json:"errors"`
}

// Lookup results of a catalogue number.
const (
	DomainPackLookupFound     = "found"
	DomainPackLookupMissing   = "missing"
	DomainPackLookupAmbiguous = "ambiguous"
)

// DomainPackDocumentView is one catalogue.yaml entry with its inspection.
type DomainPackDocumentView struct {
	DomainPackCatalogueRef
	Articles []string `json:"articles"`
	// CitedIn lists the pack files citing the number (computed at load).
	CitedIn []string `json:"cited_in"`
	// Formulas are the ids of the formulas whose basis names the number.
	Formulas []string `json:"formulas"`
	// Lookup is found, missing or ambiguous; "" when the lookup failed.
	Lookup string `json:"lookup"`
	// Documents are the catalogue rows holding the number (0, 1 or more).
	Documents []DomainPackReconcileDocument `json:"documents"`
	// Warning: what the pack says about this document needs checking
	// (missing, ambiguous, a status note, no KB holds the text).
	Warning bool `json:"warning"`
}

// DomainPackFormulaView is one formulas.yaml entry.
type DomainPackFormulaView struct {
	ID         string                    `json:"id"`
	Name       string                    `json:"name"`
	Expression string                    `json:"expression"`
	Tool       string                    `json:"tool"`
	Variables  []DomainPackFormulaVar    `json:"variables"`
	Basis      []DomainPackFormulaBasis  `json:"basis"`
	Examples   []DomainPackFormulaSample `json:"examples"`
	// Warning: one of its basis documents has a warning.
	Warning bool `json:"warning"`
	// Edited: a SuperAdmin's edit replaces the wording of formulas.yaml;
	// EditedBy / EditedAt say who and when (Shipped is the original).
	Edited   bool                   `json:"edited"`
	EditedBy string                 `json:"edited_by,omitempty"`
	EditedAt *time.Time             `json:"edited_at,omitempty"`
	Shipped  *DomainPackFormulaEdit `json:"shipped,omitempty"`
	// EditNotApplied: an edit is stored but this build refuses it (the
	// formula or the catalogue changed); formulas.yaml applies.
	EditNotApplied bool `json:"edit_not_applied,omitempty"`
}

// DomainPackFormulaEdit is a SuperAdmin's wording of a shipped formula (the
// PUT body, and the row kept in domain_pack_formula_overrides). The id,
// tool and variable names are those of formulas.yaml: the Go
// implementation is bound to them.
type DomainPackFormulaEdit struct {
	Name       string                      `json:"name"`
	Expression string                      `json:"expression"`
	Variables  []DomainPackFormulaVar      `json:"variables"`
	Basis      []DomainPackFormulaBasisRef `json:"basis"`
	Examples   []DomainPackFormulaSample   `json:"examples"`
}

// DomainPackFormulaBasisRef is one basis provision as written: số hiệu (in
// the pack's catalogue) and optionally the article.
type DomainPackFormulaBasisRef struct {
	Number  string `json:"number"`
	Article string `json:"article,omitempty"`
}

// DomainPackFormulaOverride is one stored edit, applied over the shipped
// formula at startup and on save; deleting it restores formulas.yaml.
type DomainPackFormulaOverride struct {
	PackName  string                `json:"pack_name"  gorm:"primaryKey"`
	FormulaID string                `json:"formula_id" gorm:"primaryKey"`
	Formula   DomainPackFormulaEdit `json:"formula"    gorm:"serializer:json;type:jsonb;not null"`
	UpdatedBy string                `json:"updated_by"`
	CreatedAt time.Time             `json:"created_at"`
	UpdatedAt time.Time             `json:"updated_at"`
}

// TableName names the table of DomainPackFormulaOverride.
func (DomainPackFormulaOverride) TableName() string { return "domain_pack_formula_overrides" }

// DomainPackFormulaVar is one variable of a formula.
type DomainPackFormulaVar struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Unit        string `json:"unit"`
	Source      string `json:"source"` // input | kb
}

// Basis states of a formula.
const (
	DomainPackBasisOK      = "ok"
	DomainPackBasisWarning = "warning"
)

// DomainPackFormulaBasis is one basis provision and the state of its
// document in the catalogue.
type DomainPackFormulaBasis struct {
	Number  string `json:"number"`
	Article string `json:"article,omitempty"`
	Status  string `json:"status"` // ok | warning
	Note    string `json:"note,omitempty"`
}

// DomainPackFormulaSample is a worked example.
type DomainPackFormulaSample struct {
	Inputs   map[string]float64 `json:"inputs"`
	Expected map[string]float64 `json:"expected"`
}

// DomainPackAgent is a custom agent whose config names the pack.
type DomainPackAgent struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	TenantID   uint64 `json:"tenant_id"`
	TenantName string `json:"tenant_name"`
	// AppliesToTenantIDs is the agent's "Tổ chức áp dụng" list ([] = every
	// Tổ chức on its configuration profile); AppliesTo names them.
	AppliesToTenantIDs AgentTenantScope       `json:"applies_to_tenant_ids"`
	AppliesTo          []DomainPackAgentScope `json:"applies_to" gorm:"-"`
	// Distribution is the agent's "Cách phân phối" (basic | addon |
	// internal); the list above only counts for internal.
	Distribution string `json:"distribution"`
	// AddonPriceVND and AddonActive are the agent's row in the add-on
	// catalogue (usage_agent_addons); nil = not in the catalogue.
	// AddonHolders counts the current subscriptions holding the add-on.
	AddonPriceVND *int64 `json:"addon_price_vnd"`
	AddonActive   *bool  `json:"addon_active"`
	AddonHolders  int64  `json:"addon_holders" gorm:"-"`
}

// DomainPackAgentScope is one Tổ chức in an agent's "Tổ chức áp dụng" list.
type DomainPackAgentScope struct {
	TenantID   uint64 `json:"tenant_id"`
	TenantName string `json:"tenant_name"`
}
