package types

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
