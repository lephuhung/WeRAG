package types

import (
	"strings"
	"time"
)

// Catalogue status wordings (vbpl.vn's) the validity logic keys on.
const (
	LegalStatusInForce        = "Còn hiệu lực"
	LegalStatusPartlyExpired  = "Hết hiệu lực một phần"
	LegalStatusExpired        = "Hết hiệu lực toàn bộ"
	LegalStatusMaybeExpired   = "Có thể đã hết hiệu lực toàn bộ"
	LegalStatusNotYetInForce  = "Chưa có hiệu lực"
	legalStatusUnknownLowered = "chưa xác định"
)

// LegalStatusKind is a catalogue status reduced to the few states a reader
// acts on; the source wording stays next to it.
type LegalStatusKind string

const (
	LegalStatusKindInForce       LegalStatusKind = "in_force"
	LegalStatusKindExpired       LegalStatusKind = "expired"
	LegalStatusKindPartlyExpired LegalStatusKind = "partly_expired"
	LegalStatusKindNotYetInForce LegalStatusKind = "not_yet_in_force"
	LegalStatusKindSuspended     LegalStatusKind = "suspended"
	LegalStatusKindUnknown       LegalStatusKind = "unknown"
)

// legalStatusKey is a status wording lower-cased with single spaces, the
// form every rule here compares.
func legalStatusKey(status string) string {
	return strings.ToLower(strings.Join(strings.Fields(status), " "))
}

// ClassifyLegalStatus reduces a catalogue status wording to its kind. An
// empty or unrecognised wording is unknown — never expired. A partial
// suspension ("Ngưng hiệu lực một phần") is suspended here; the legal basis
// check refines it (tools.classifyLegal).
func ClassifyLegalStatus(status string) LegalStatusKind {
	s := legalStatusKey(status)
	switch {
	case strings.HasPrefix(s, "còn hiệu lực"):
		return LegalStatusKindInForce
	case strings.HasPrefix(s, "hết hiệu lực một phần"):
		return LegalStatusKindPartlyExpired
	case strings.HasPrefix(s, "hết hiệu lực"):
		return LegalStatusKindExpired
	case strings.HasPrefix(s, "chưa có hiệu lực"):
		return LegalStatusKindNotYetInForce
	case strings.HasPrefix(s, "ngưng"), strings.HasPrefix(s, "tạm ngưng"), strings.HasPrefix(s, "đình chỉ"):
		return LegalStatusKindSuspended
	}
	return LegalStatusKindUnknown
}

// LegalDerivableStatus reports whether a source status may be reconciled
// with the catalogue's links: in force, unknown, or partly expired (worded
// softer). Any other status (expired, not yet in force, suspended) is left
// as is.
func LegalDerivableStatus(status string) bool {
	switch legalStatusKey(status) {
	case "", legalStatusUnknownLowered, strings.ToLower(LegalStatusInForce), strings.ToLower(LegalStatusPartlyExpired):
		return true
	}
	return false
}

// LegalPartlyExpired reports whether the source holds doc partly expired.
func LegalPartlyExpired(doc *LegalDocument) bool {
	return doc != nil && legalStatusKey(doc.Status) == strings.ToLower(LegalStatusPartlyExpired)
}

// LegalDerivedStatusText is the status LegalDerivedExpiry implies for doc:
// expired as a whole, or only maybe so when the portal judged it partly
// expired (the replacement may be partial too).
func LegalDerivedStatusText(doc *LegalDocument) string {
	if LegalPartlyExpired(doc) {
		return LegalStatusMaybeExpired
	}
	return LegalStatusExpired
}

// legalDerivedExpiryRelations are the incoming relations that end a
// document as a whole once the other document takes effect. Repeals is left
// out: vbpl files partial repeals under it too ("Bị bãi bỏ (toàn bộ hoặc
// một phần)"), and PartiallyExpires is partial by definition.
var legalDerivedExpiryRelations = map[LegalRelation]bool{
	LegalRelationReplaces: true,
	LegalRelationExpires:  true,
}

// LegalDerivedExpiry reconciles a stale portal status with the catalogue's
// own links: the source has no expiry date and does not say the document
// ended, but an incoming replaces/expires link points at a document already
// in force (its effective day is today or earlier, LegalDateReached). It
// returns the earliest such document and its effective date, the day this
// one ended; nil when there is none. vbpl still listed 29/2018/QH14 as
// "Còn hiệu lực" months after 117/2025/QH15 replaced it (2026-10-16).
func LegalDerivedExpiry(rep *LegalStatusReport, now time.Time) (*LegalDocument, *time.Time) {
	if rep == nil || rep.Document == nil || rep.Document.ExpiredOn != nil || !LegalDerivableStatus(rep.Document.Status) {
		return nil, nil
	}
	var by *LegalDocument
	for _, l := range rep.Links {
		if !l.Incoming || !legalDerivedExpiryRelations[l.Relation] || l.Other == nil || !LegalDateReached(l.Other.EffectiveOn, now) {
			continue
		}
		if by == nil || LegalDateString(l.Other.EffectiveOn) < LegalDateString(by.EffectiveOn) {
			by = l.Other
		}
	}
	if by == nil {
		return nil, nil
	}
	return by, by.EffectiveOn
}

// LegalValidity is a catalogue document's validity on a day, the part every
// reader shares (the validity lookup, check_legal_status's related
// documents, the legal basis check): the kind its wording says
// (ClassifyLegalStatus), turned expired when LegalDerivedExpiry shows the
// wording is stale. A partly expired document stays partly expired — the
// replacement may be partial too — and DerivedBy then only means "maybe
// wholly expired".
type LegalValidity struct {
	Kind      LegalStatusKind
	DerivedBy *LegalDocument
	DerivedOn *time.Time
}

// ClassifyLegalValidity states rep's document's validity as of now (rep.Links
// are its incoming links, may be empty). The legal basis check adds its own
// layers on top (dates, amendments, documents needing a review).
func ClassifyLegalValidity(rep *LegalStatusReport, now time.Time) LegalValidity {
	if rep == nil || rep.Document == nil {
		return LegalValidity{Kind: LegalStatusKindUnknown}
	}
	v := LegalValidity{Kind: ClassifyLegalStatus(rep.Document.Status)}
	if by, on := LegalDerivedExpiry(rep, now); by != nil {
		v.DerivedBy, v.DerivedOn = by, on
		if v.Kind != LegalStatusKindPartlyExpired {
			v.Kind = LegalStatusKindExpired
		}
	}
	return v
}

// LegalDerivedExpiryRelation is the relation by which `by` (as returned by
// LegalDerivedExpiry) ended rep's document.
func LegalDerivedExpiryRelation(rep *LegalStatusReport, by *LegalDocument) LegalRelation {
	for _, l := range rep.Links {
		if l.Other == by && l.Incoming && legalDerivedExpiryRelations[l.Relation] {
			return l.Relation
		}
	}
	return LegalRelationReplaces
}

// LegalValidityLookup answers the validity lookup page (no chat, no LLM):
// the catalogue documents a số hiệu or title matches, newest first.
type LegalValidityLookup struct {
	Query string `json:"query"`
	// By is how the query matched: "number", "title" or "" (nothing).
	By      string               `json:"by"`
	Matches []LegalValidityMatch `json:"matches"`
	// More reports that more documents match than Matches lists.
	More bool `json:"more"`
	// NumberMissing is the số hiệu the query named when the catalogue has
	// no such document: Matches then only mention it in their title.
	NumberMissing string `json:"number_missing,omitempty"`
	// CatalogSnapshotDate is the date (YYYY-MM-DD) of the newest vbpl.vn
	// snapshot the catalogue holds, empty when unknown.
	CatalogSnapshotDate string `json:"catalog_snapshot_date"`
}

// LegalValidityMatch is one catalogue document with its validity. Dates
// are YYYY-MM-DD, empty when the source gives none.
type LegalValidityMatch struct {
	LegalValidityRef
	Issuer string `json:"issuer"`
	// StatusText is the source's wording ("Còn hiệu lực", …); Status its
	// kind after reconciling with the links (Derived).
	StatusText string `json:"status_text"`
	ExpiredOn  string `json:"expired_on"`
	// Derived is set when an in-force replacing or ending document shows
	// the source status is stale.
	Derived *LegalDerivedValidity `json:"derived,omitempty"`
	Source  string                `json:"source"`
	// SourceLabel names the source for a reader ("vbpl", "thuvienphapluat.vn").
	SourceLabel string `json:"source_label"`
	// SourceUpdatedOn is the date of the source snapshot this row reflects.
	SourceUpdatedOn string `json:"source_updated_on"`
	// Unverified: a feed row no SuperAdmin confirmed; UnverifiedNote says so.
	Unverified     bool   `json:"unverified"`
	UnverifiedNote string `json:"unverified_note,omitempty"`
	// ReplacedBy, EndedBy (repealed, made to expire, suspended) and
	// AmendedBy (amended, corrected) list the documents acting on this one,
	// newest first, at most a few each; the *Total fields count them all.
	ReplacedBy      []LegalValidityRef `json:"replaced_by"`
	ReplacedByTotal int                `json:"replaced_by_total"`
	EndedBy         []LegalValidityRef `json:"ended_by"`
	EndedByTotal    int                `json:"ended_by_total"`
	AmendedBy       []LegalValidityRef `json:"amended_by"`
	AmendedByTotal  int                `json:"amended_by_total"`
	// Knowledge lists the copies the caller can open in the app.
	Knowledge []LegalKnowledgeRef `json:"knowledge"`
}

// LegalValidityRef names a catalogue document briefly.
type LegalValidityRef struct {
	ID             string          `json:"id"`
	DocumentNumber string          `json:"document_number"`
	Title          string          `json:"title"`
	DocType        string          `json:"doc_type"`
	IssuedOn       string          `json:"issued_on"`
	EffectiveOn    string          `json:"effective_on"`
	Status         LegalStatusKind `json:"status"`
	// Relation is set on a related document: what it did to the match.
	Relation LegalRelation `json:"relation,omitempty"`
}

// LegalDerivedValidity is the reconciled status of a match: StatusText
// ("Hết hiệu lực toàn bộ" / "Có thể đã hết hiệu lực toàn bộ") since
// ExpiredOn, by the document By through Relation.
type LegalDerivedValidity struct {
	StatusText string           `json:"status_text"`
	ExpiredOn  string           `json:"expired_on"`
	Relation   LegalRelation    `json:"relation"`
	By         LegalValidityRef `json:"by"`
}

// LegalDateString formats a catalogue date as YYYY-MM-DD ("" for none). A
// catalogue date is a calendar day (a date column, read back as midnight):
// it is formatted as written, in its own location.
func LegalDateString(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.Format("2006-01-02")
}

// legalDayZone is the calendar the validity rules count days in: a
// document takes effect or ends on a Vietnam calendar day, whatever the
// server clock's zone.
var legalDayZone = func() *time.Location {
	if l, err := time.LoadLocation("Asia/Ho_Chi_Minh"); err == nil {
		return l
	}
	return time.FixedZone("ICT", 7*3600)
}()

// LegalToday is now's Vietnam calendar day, YYYY-MM-DD.
func LegalToday(now time.Time) string { return now.In(legalDayZone).Format("2006-01-02") }

// LegalDateReached reports whether a catalogue date is set and falls on or
// before now's Vietnam day: a document effective "today" is in force all
// day, one expiring today has ended.
func LegalDateReached(d *time.Time, now time.Time) bool {
	return d != nil && !d.IsZero() && LegalDateString(d) <= LegalToday(now)
}

// LegalDateAhead reports whether a catalogue date is set and falls after
// now's Vietnam day.
func LegalDateAhead(d *time.Time, now time.Time) bool {
	return d != nil && !d.IsZero() && LegalDateString(d) > LegalToday(now)
}

// LegalCatalogFilter narrows a catalogue search; every set field must hold.
// Number keys come from vietnamese_legal.CatalogNumberKey. DocTypes match
// case-insensitively. IssuedYear holds when the document was issued that
// year or its number names it ("…/2024/…").
type LegalCatalogFilter struct {
	NumberKey    string
	NumberPrefix string // number_key starts with NumberPrefix + "/"
	TitleWords   []string
	DocTypes     []string
	IssuedYear   int
	Limit        int
}

// LegalLookupMiss is a validity lookup that found nothing (or found only
// documents mentioning a số hiệu the catalogue lacks), counted per
// normalized query so a SuperAdmin sees the catalogue's gaps.
type LegalLookupMiss struct {
	ID uint64 `json:"id" gorm:"primaryKey;autoIncrement"`
	// Query is the normalized query (lower-case, single spaces), unique.
	Query string `json:"query" gorm:"type:varchar(200);not null;uniqueIndex"`
	// Sample is one raw query as typed.
	Sample string `json:"sample" gorm:"type:varchar(200);not null;default:''"`
	// Number is the số hiệu the query named, if any.
	Number string `json:"number" gorm:"type:varchar(128);not null;default:''"`
	// Reason is LegalLookupMissNotFound or LegalLookupMissNumber.
	Reason  string    `json:"reason" gorm:"type:varchar(16);not null;default:''"`
	Count   int64     `json:"count" gorm:"not null;default:1"`
	FirstAt time.Time `json:"first_at"`
	LastAt  time.Time `json:"last_at"`
	// LastTenantID / LastUserID stay in the database for support; the
	// SuperAdmin list does not show them.
	LastTenantID uint64 `json:"-" gorm:"not null;default:0"`
	LastUserID   string `json:"-" gorm:"type:varchar(36);not null;default:''"`
}

// TableName returns the table name for LegalLookupMiss.
func (LegalLookupMiss) TableName() string { return "legal_lookup_misses" }

const (
	// LegalLookupMissNotFound: nothing in the catalogue matched.
	LegalLookupMissNotFound = "not_found"
	// LegalLookupMissNumber: the số hiệu is not in the catalogue, but
	// documents mentioning it in their title were found.
	LegalLookupMissNumber = "number_missing"
)
