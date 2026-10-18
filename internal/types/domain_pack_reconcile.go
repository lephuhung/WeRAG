package types

// DomainPackRequestSourcePrefix marks a legal document request recorded by
// the reconcile of a domain pack's catalogue ("pack:<name>" in
// LegalDocumentRequest.Source), as opposed to a chat question.
const DomainPackRequestSourcePrefix = "pack:"

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
	// StatusNote is set when the catalogue holds the document expired or
	// replaced: the pack relies on a document that no longer applies.
	StatusNote string `json:"status_note,omitempty"`
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
	RequestID     string `json:"request_id,omitempty"`
	RequestStatus string `json:"request_status,omitempty"`
	// RequestCreated is true when this reconcile recorded the request,
	// false when an existing one (any status) was found and left as is.
	RequestCreated bool `json:"request_created"`
}
