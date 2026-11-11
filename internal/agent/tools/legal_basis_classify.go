package tools

import (
	"strings"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
)

// The legal basis check's classifier: one catalogue document and its
// incoming links → a status as of a day. Kept apart from the scan and the
// lookups so it can move onto a shared validity classifier later.

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

// legalDayAfter reports whether t falls on a later calendar day than at,
// each read in its own location (catalogue dates are UTC midnights).
func legalDayAfter(t, at time.Time) bool { return t.Format("2006-01-02") > at.Format("2006-01-02") }

// legalInForceOn reports whether a document acting on another is in force
// on now by its dates: effective (or, without an effective date, issued)
// on or before now and not expired.
func legalInForceOn(d *types.LegalDocument, now time.Time) bool {
	if d == nil {
		return false
	}
	if d.ExpiredOn != nil && !d.ExpiredOn.IsZero() && !legalDayAfter(*d.ExpiredOn, now) {
		return false
	}
	switch {
	case d.EffectiveOn != nil && !d.EffectiveOn.IsZero():
		return !legalDayAfter(*d.EffectiveOn, now)
	case d.IssuedOn != nil && !d.IssuedOn.IsZero():
		return !legalDayAfter(*d.IssuedOn, now)
	}
	return false
}

// ClassifyLegalDocument states a catalogue document's status as of now
// (see classifyLegal); rep.Links are its incoming links (may be empty).
func ClassifyLegalDocument(rep *types.LegalStatusReport, now time.Time) LegalBasisStatus {
	return classifyLegal(rep, now).status
}

// classifyLegal applies, in order:
//   - a partial status first, whatever the dates say: "Hết hiệu lực một
//     phần" → partly expired (expired_on is then the date of the partial
//     expiry); "Ngưng hiệu lực một phần" or any other "… một phần" →
//     needs review;
//   - expired: "Hết hiệu lực toàn bộ" (any "Hết hiệu lực…" left after the
//     partial branch), or the expiry date has passed;
//   - suspended: "Ngưng hiệu lực" / "Tạm ngưng hiệu lực" (whole);
//   - expired by derivation: a document already in force replaces or ends
//     it while the catalogue holds it in force or states nothing
//     (check_legal_status derivedExpiry);
//   - not yet effective: "Chưa có hiệu lực" with no effective date or one
//     ahead, or any status with an effective date ahead; a stale "Chưa có
//     hiệu lực" whose effective date has passed is read as in force;
//   - needs review: "Không còn phù hợp" (the portal's flag, not an
//     expiry); in force (or no status) while a document in force repeals
//     it or ends it in part — a repeal alone is never an expiry;
//   - amended: in force with an amending document; in force; unknown
//     ("Chưa xác định", "" or any other wording, when nothing above
//     decides).
func classifyLegal(rep *types.LegalStatusReport, now time.Time) legalClassification {
	d := rep.Document
	status := strings.ToLower(strings.Join(strings.Fields(d.Status), " "))
	var c legalClassification
	if strings.Contains(status, "một phần") {
		if strings.HasPrefix(status, "hết hiệu lực một phần") {
			c.status = LegalBasisPartlyExpired
			c.derivedBy, c.derivedOn = derivedExpiry(rep, now)
			return c
		}
		c.status = LegalBasisNeedsReview
		c.partialSuspension = strings.Contains(status, "ngưng") || strings.Contains(status, "đình chỉ")
		return c
	}
	if strings.HasPrefix(status, "hết hiệu lực") ||
		d.ExpiredOn != nil && !d.ExpiredOn.IsZero() && !legalDayAfter(*d.ExpiredOn, now) {
		c.status = LegalBasisExpired
		return c
	}
	if strings.Contains(status, "ngưng hiệu lực") || strings.Contains(status, "đình chỉ") {
		c.status = LegalBasisSuspended
		return c
	}
	if by, on := derivedExpiry(rep, now); by != nil {
		c.status, c.derivedBy, c.derivedOn = LegalBasisExpired, by, on
		return c
	}
	effectiveAhead := d.EffectiveOn != nil && !d.EffectiveOn.IsZero() && legalDayAfter(*d.EffectiveOn, now)
	switch {
	case effectiveAhead:
		c.status = LegalBasisNotYetEffective
		return c
	case status == "chưa có hiệu lực":
		if d.EffectiveOn == nil || d.EffectiveOn.IsZero() {
			c.status = LegalBasisNotYetEffective
			return c
		}
		c.staleNotYet = true
	}
	if status == "không còn phù hợp" {
		c.status, c.unsuitable = LegalBasisNeedsReview, true
		return c
	}
	inForce := status == "còn hiệu lực" || c.staleNotYet
	if !inForce && status != "" && status != "chưa xác định" {
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
