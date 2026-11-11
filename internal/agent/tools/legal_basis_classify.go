package tools

import (
	"strings"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
)

// The legal basis check's classifier: one catalogue document and its
// incoming links → a status as of a day. It builds on the validity every
// reader shares (types.ClassifyLegalValidity: the wording's kind, expired
// when an in-force replacement shows the wording is stale) and adds the
// check's own layers: dates, amendments, documents needing a review.
// Days are Vietnam calendar days (types.LegalDateReached).

// legalClassification is a document's status and what it rests on.
type legalClassification struct {
	status LegalBasisStatus
	// derivedBy / derivedOn: a document already in force replaces or ends
	// this one while the catalogue status says otherwise (derivedExpiry).
	// With a partly-expired status it only means "maybe wholly expired".
	derivedBy *types.LegalDocument
	derivedOn *time.Time
	// reviewBy: documents in force that repeal or end this one in part (or
	// repeal it, which vbpl files for partial repeals too) while the
	// catalogue holds it in force: needs_review, never expired.
	reviewBy []*types.LegalDocument
	// partialSuspension: the catalogue says "Ngưng hiệu lực một phần".
	partialSuspension bool
	// unsuitable: the portal says "Không còn phù hợp".
	unsuitable bool
	// staleNotYet: the catalogue says "Chưa có hiệu lực" but its effective
	// date has passed; the date is trusted.
	staleNotYet bool
}

// legalInForceOn reports whether a document acting on another is in force
// on now by its dates: effective (or, without an effective date, issued)
// on or before now and not expired.
func legalInForceOn(d *types.LegalDocument, now time.Time) bool {
	if d == nil || types.LegalDateReached(d.ExpiredOn, now) {
		return false
	}
	switch {
	case d.EffectiveOn != nil && !d.EffectiveOn.IsZero():
		return types.LegalDateReached(d.EffectiveOn, now)
	case d.IssuedOn != nil && !d.IssuedOn.IsZero():
		return types.LegalDateReached(d.IssuedOn, now)
	}
	return false
}

// ClassifyLegalDocument states a catalogue document's status as of now
// (see classifyLegal); rep.Links are its incoming links (may be empty).
func ClassifyLegalDocument(rep *types.LegalStatusReport, now time.Time) LegalBasisStatus {
	return classifyLegal(rep, now).status
}

// LegalBasisStatusOfKind is the legal basis status a shared validity kind
// (types.LegalStatusKind) reads as before the check's own layers.
func LegalBasisStatusOfKind(k types.LegalStatusKind) LegalBasisStatus {
	switch k {
	case types.LegalStatusKindInForce:
		return LegalBasisInForce
	case types.LegalStatusKindExpired:
		return LegalBasisExpired
	case types.LegalStatusKindPartlyExpired:
		return LegalBasisPartlyExpired
	case types.LegalStatusKindNotYetInForce:
		return LegalBasisNotYetInForce
	case types.LegalStatusKindSuspended:
		return LegalBasisSuspended
	}
	return LegalBasisUnknown
}

// classifyLegal starts from the shared validity (types.ClassifyLegalValidity)
// and applies, in order:
//   - a partial wording first, whatever the dates say: partly expired stays
//     partly expired (expired_on is then the date of the partial expiry, a
//     derived replacement only "maybe wholly expired"); "Ngưng hiệu lực một
//     phần" or any other "… một phần" → needs review (the shared kind says
//     suspended);
//   - expired: by wording, or the expiry date has passed (a date layer: the
//     shared kind does not read dates);
//   - suspended (whole);
//   - expired by derivation (the shared kind);
//   - not yet in force: the effective date is ahead, whatever the wording
//     ("Còn hiệu lực", none), or "Chưa có hiệu lực" with no effective date;
//     a stale "Chưa có hiệu lực" whose effective date has passed is read as
//     in force (an in-force replacement then ends it, as derived expiry);
//   - needs review: "Không còn phù hợp" (the portal's flag, not an expiry;
//     the shared kind says unknown); in force (or no status) while a
//     document in force repeals it or ends it in part — a repeal alone is
//     never an expiry;
//   - amended: in force with an amending document; in force; unknown
//     ("Chưa xác định", "" or any other wording, when nothing above
//     decides).
func classifyLegal(rep *types.LegalStatusReport, now time.Time) legalClassification {
	d := rep.Document
	status := strings.ToLower(strings.Join(strings.Fields(d.Status), " "))
	v := types.ClassifyLegalValidity(rep, now)
	var c legalClassification
	if strings.Contains(status, "một phần") {
		if v.Kind == types.LegalStatusKindPartlyExpired {
			c.status = LegalBasisPartlyExpired
			c.derivedBy, c.derivedOn = v.DerivedBy, v.DerivedOn
			return c
		}
		c.status = LegalBasisNeedsReview
		c.partialSuspension = strings.Contains(status, "ngưng") || strings.Contains(status, "đình chỉ")
		return c
	}
	switch {
	case v.Kind == types.LegalStatusKindExpired && v.DerivedBy == nil, types.LegalDateReached(d.ExpiredOn, now):
		c.status = LegalBasisExpired
		return c
	case v.Kind == types.LegalStatusKindSuspended:
		c.status = LegalBasisSuspended
		return c
	case v.DerivedBy != nil:
		c.status, c.derivedBy, c.derivedOn = LegalBasisExpired, v.DerivedBy, v.DerivedOn
		return c
	case types.LegalDateAhead(d.EffectiveOn, now):
		c.status = LegalBasisNotYetInForce
		return c
	case v.Kind == types.LegalStatusKindNotYetInForce:
		if d.EffectiveOn == nil || d.EffectiveOn.IsZero() {
			c.status = LegalBasisNotYetInForce
			return c
		}
		c.staleNotYet = true
		// read as in force, it is ended by a replacement already in force
		// as an in-force document would be
		asInForce := *d
		asInForce.Status = types.LegalStatusInForce
		if by, on := types.LegalDerivedExpiry(&types.LegalStatusReport{Document: &asInForce, Links: rep.Links}, now); by != nil {
			c.status, c.derivedBy, c.derivedOn = LegalBasisExpired, by, on
			return c
		}
	}
	unsuitable := status == "không còn phù hợp"
	inForce := v.Kind == types.LegalStatusKindInForce || c.staleNotYet
	if !inForce && !unsuitable && status != "" && status != "chưa xác định" {
		c.status = LegalBasisUnknown
		return c
	}
	for _, l := range rep.Links {
		if !l.Incoming || l.Other == nil {
			continue
		}
		switch l.Relation {
		case types.LegalRelationRepeals, types.LegalRelationPartiallyExpires, types.LegalRelationExpires:
			if legalInForceOn(l.Other, now) {
				c.reviewBy = append(c.reviewBy, l.Other)
			}
		}
	}
	switch {
	case unsuitable:
		// the portal's flag; repealing documents are still named
		c.status, c.unsuitable = LegalBasisNeedsReview, true
	case len(c.reviewBy) > 0:
		c.status = LegalBasisNeedsReview
	case !inForce:
		c.status = LegalBasisUnknown
	default:
		c.status = LegalBasisInForce
		for _, l := range rep.Links {
			if l.Incoming && l.Relation == types.LegalRelationAmends {
				c.status = LegalBasisAmended
				break
			}
		}
	}
	return c
}
