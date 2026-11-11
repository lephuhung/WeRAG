package service

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/agent/tools"
	"github.com/Tencent/WeKnora/internal/types"
)

// One status classifier (M6): the validity lookup (legalValidityMatch, the
// kind check_legal_status and the lookup page share) and the legal basis
// check (tools.ClassifyLegalDocument) must agree on every catalogue wording
// and date/link combination. The legal basis check may only differ through
// its own layers, each named here; everywhere else its status read back as
// a kind is the lookup's kind.

var agreementVN = time.FixedZone("ICT", 7*3600)

// agreementDay is a catalogue date as the repository returns it: midnight UTC.
func agreementDay(s string) *time.Time {
	if s == "" {
		return nil
	}
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		panic(err)
	}
	return &t
}

// agreementWordings are the status values the dev catalogue holds, plus
// spacing and wording variants.
var agreementWordings = []string{
	"Còn hiệu lực", "Hết hiệu lực toàn bộ", "Chưa xác định", "Hết hiệu lực một phần", "",
	"Ngưng hiệu lực", "Chưa có hiệu lực", "Không còn phù hợp", "Ngưng hiệu lực một phần",
	"Tạm ngưng hiệu lực", "  còn   hiệu lực ", "Hết hiệu lực",
}

// basisKind reads a legal basis status back as the shared kind: what it
// says before the check's own refinements (amended is in force).
func basisKind(s tools.LegalBasisStatus) types.LegalStatusKind {
	switch s {
	case tools.LegalBasisInForce, tools.LegalBasisAmended:
		return types.LegalStatusKindInForce
	case tools.LegalBasisExpired:
		return types.LegalStatusKindExpired
	case tools.LegalBasisPartlyExpired:
		return types.LegalStatusKindPartlyExpired
	case tools.LegalBasisNotYetInForce:
		return types.LegalStatusKindNotYetInForce
	case tools.LegalBasisSuspended:
		return types.LegalStatusKindSuspended
	}
	return types.LegalStatusKindUnknown
}

func TestLegalStatusClassifiersAgree(t *testing.T) {
	// 03:00 in Vietnam is still the previous day in UTC: the catalogue's
	// "2026-11-11" is today for both classifiers at both clocks
	clocks := []time.Time{
		time.Date(2026, 11, 11, 9, 0, 0, 0, agreementVN),
		time.Date(2026, 11, 11, 3, 0, 0, 0, agreementVN),
	}
	const past, today, future = "2025-01-01", "2026-11-11", "2026-11-12"
	dates := []struct{ expired, effective string }{
		{"", ""}, {past, ""}, {today, ""}, {future, ""},
		{"", past}, {"", today}, {"", future}, {future, past},
	}
	other := func(id, status, effective string) *types.LegalDocument {
		return &types.LegalDocument{ID: id, DocumentNumber: id + "/2026/NĐ-CP", DocType: "Nghị định",
			Status: status, IssuedOn: agreementDay("2025-06-01"), EffectiveOn: agreementDay(effective)}
	}
	linkSets := []struct {
		name  string
		links []types.LegalRelationLink
	}{
		{"none", nil},
		{"replaced, in force", []types.LegalRelationLink{{Relation: types.LegalRelationReplaces, Incoming: true, Other: other("r1", "Còn hiệu lực", past)}}},
		{"replaced today", []types.LegalRelationLink{{Relation: types.LegalRelationReplaces, Incoming: true, Other: other("r2", "Còn hiệu lực", today)}}},
		{"replaced tomorrow", []types.LegalRelationLink{{Relation: types.LegalRelationReplaces, Incoming: true, Other: other("r3", "Chưa có hiệu lực", future)}}},
		{"ended, in force", []types.LegalRelationLink{{Relation: types.LegalRelationExpires, Incoming: true, Other: other("e1", "Còn hiệu lực", past)}}},
		{"repealed, in force", []types.LegalRelationLink{{Relation: types.LegalRelationRepeals, Incoming: true, Other: other("p1", "Còn hiệu lực", past)}}},
		{"partly ended, in force", []types.LegalRelationLink{{Relation: types.LegalRelationPartiallyExpires, Incoming: true, Other: other("p2", "Còn hiệu lực", past)}}},
		{"amended", []types.LegalRelationLink{{Relation: types.LegalRelationAmends, Incoming: true, Other: other("a1", "Còn hiệu lực", past)}}},
	}
	plain := 0
	for _, now := range clocks {
		for _, wording := range agreementWordings {
			for _, dt := range dates {
				for _, ls := range linkSets {
					d := &types.LegalDocument{ID: "x", DocumentNumber: "1/2020/NĐ-CP", DocType: "Nghị định", Status: wording,
						IssuedOn: agreementDay("2020-01-01"), ExpiredOn: agreementDay(dt.expired), EffectiveOn: agreementDay(dt.effective)}
					rep := &types.LegalStatusReport{Document: d, Links: ls.links}
					lookup := legalValidityMatch(rep, now, nil).Status
					basis := tools.ClassifyLegalDocument(rep, now)
					want, layer := expectedBasis(lookup, rep, now)
					if layer == "" {
						plain++
					}
					name := fmt.Sprintf("%s | %q expired=%q effective=%q links=%s", now.Format("15:04"), wording, dt.expired, dt.effective, ls.name)
					if basis != want {
						t.Errorf("%s: lookup %s, legal basis %s, want %s (layer %q)", name, lookup, basis, want, layer)
					}
					if layer == "" && basisKind(basis) != lookup {
						t.Errorf("%s: no layer applies, yet legal basis %s reads as %s, lookup %s", name, basis, basisKind(basis), lookup)
					}
				}
			}
		}
	}
	if plain < 100 {
		t.Fatalf("only %d combinations ran through the shared part alone", plain)
	}
}

// expectedBasis is the legal basis status for a document the lookup
// classified as lookup, and the name of the legal basis layer that decided
// it ("" when the shared kind alone decides).
func expectedBasis(lookup types.LegalStatusKind, rep *types.LegalStatusReport, now time.Time) (tools.LegalBasisStatus, string) {
	d := rep.Document
	wording := strings.ToLower(strings.Join(strings.Fields(d.Status), " "))
	if strings.Contains(wording, "một phần") && lookup != types.LegalStatusKindPartlyExpired {
		return tools.LegalBasisNeedsReview, "partial wording"
	}
	if lookup == types.LegalStatusKindPartlyExpired {
		return tools.LegalBasisPartlyExpired, ""
	}
	if types.LegalDateReached(d.ExpiredOn, now) && lookup != types.LegalStatusKindExpired {
		return tools.LegalBasisExpired, "expiry date passed"
	}
	switch lookup {
	case types.LegalStatusKindExpired:
		return tools.LegalBasisExpired, ""
	case types.LegalStatusKindSuspended:
		return tools.LegalBasisSuspended, ""
	}
	if types.LegalDateAhead(d.EffectiveOn, now) {
		if lookup == types.LegalStatusKindNotYetInForce {
			return tools.LegalBasisNotYetInForce, ""
		}
		return tools.LegalBasisNotYetInForce, "effective date ahead"
	}
	base, layer := lookup, ""
	if lookup == types.LegalStatusKindNotYetInForce {
		if d.EffectiveOn == nil {
			return tools.LegalBasisNotYetInForce, ""
		}
		// stale "Chưa có hiệu lực": read as in force
		base, layer = types.LegalStatusKindInForce, "stale not yet in force"
		inForce := *d
		inForce.Status = types.LegalStatusInForce
		if by, _ := types.LegalDerivedExpiry(&types.LegalStatusReport{Document: &inForce, Links: rep.Links}, now); by != nil {
			return tools.LegalBasisExpired, layer
		}
	}
	if wording == "không còn phù hợp" {
		return tools.LegalBasisNeedsReview, "unsuitable"
	}
	if base == types.LegalStatusKindUnknown && wording != "" && wording != "chưa xác định" {
		return tools.LegalBasisUnknown, layer
	}
	for _, l := range rep.Links {
		switch l.Relation {
		case types.LegalRelationRepeals, types.LegalRelationPartiallyExpires, types.LegalRelationExpires:
			o := l.Other
			if !types.LegalDateReached(o.ExpiredOn, now) && types.LegalDateReached(o.EffectiveOn, now) {
				return tools.LegalBasisNeedsReview, "repealed or partly ended by a document in force"
			}
		}
	}
	if base == types.LegalStatusKindUnknown {
		return tools.LegalBasisUnknown, layer
	}
	for _, l := range rep.Links {
		if l.Relation == types.LegalRelationAmends {
			return tools.LegalBasisAmended, layer
		}
	}
	return tools.LegalBasisInForce, layer
}

// The Vietnam day, not the instant: a replacement effective "2026-11-11"
// (stored as midnight UTC) is in force at 03:00 on the 11th in Vietnam,
// still the 10th in UTC.
func TestLegalDerivedExpiryCountsVietnamDays(t *testing.T) {
	d := &types.LegalDocument{ID: "x", Status: "Còn hiệu lực"}
	by := &types.LegalDocument{ID: "y", EffectiveOn: agreementDay("2026-11-11")}
	rep := &types.LegalStatusReport{Document: d, Links: []types.LegalRelationLink{{Relation: types.LegalRelationReplaces, Incoming: true, Other: by}}}
	early := time.Date(2026, 11, 10, 20, 0, 0, 0, time.UTC) // 03:00 on the 11th in Vietnam
	if got, _ := types.LegalDerivedExpiry(rep, early); got != by {
		t.Errorf("effective today in Vietnam: not derived")
	}
	late := time.Date(2026, 11, 10, 16, 59, 0, 0, time.UTC) // 23:59 on the 10th in Vietnam
	if got, _ := types.LegalDerivedExpiry(rep, late); got != nil {
		t.Errorf("effective tomorrow in Vietnam: derived")
	}
	if s := tools.ClassifyLegalDocument(rep, early); s != tools.LegalBasisExpired {
		t.Errorf("legal basis at 03:00 Vietnam: %s", s)
	}
	if k := legalValidityMatch(rep, early, nil).Status; k != types.LegalStatusKindExpired {
		t.Errorf("lookup at 03:00 Vietnam: %s", k)
	}
}
