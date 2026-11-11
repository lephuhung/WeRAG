package types

import (
	"testing"
	"time"
)

func TestClassifyLegalStatus(t *testing.T) {
	cases := map[string]LegalStatusKind{
		"Còn hiệu lực":                 LegalStatusKindInForce,
		"  còn  hiệu lực ":             LegalStatusKindInForce,
		"Hết hiệu lực toàn bộ":         LegalStatusKindExpired,
		"Hết hiệu lực":                 LegalStatusKindExpired,
		"Hết hiệu lực một phần":        LegalStatusKindPartlyExpired,
		"Chưa có hiệu lực":             LegalStatusKindNotYetInForce,
		"Ngưng hiệu lực":               LegalStatusKindSuspended,
		"Tạm ngưng hiệu lực":           LegalStatusKindSuspended,
		"Đình chỉ, tạm ngưng hiệu lực": LegalStatusKindSuspended,
		"Chưa xác định":                LegalStatusKindUnknown,
		"":                             LegalStatusKindUnknown,
		"Không rõ":                     LegalStatusKindUnknown,
	}
	for in, want := range cases {
		if got := ClassifyLegalStatus(in); got != want {
			t.Errorf("ClassifyLegalStatus(%q) = %s, want %s", in, got, want)
		}
	}
}

func TestLegalDerivedExpiry(t *testing.T) {
	day := func(s string) *time.Time { d, _ := time.Parse("2006-01-02", s); return &d }
	now := *day("2026-10-16")
	newLaw := &LegalDocument{ID: "new", EffectiveOn: day("2026-01-01")}
	future := &LegalDocument{ID: "future", EffectiveOn: day("2027-01-01")}
	rep := &LegalStatusReport{
		Document: &LegalDocument{ID: "old", Status: LegalStatusInForce},
		Links: []LegalRelationLink{
			{Relation: LegalRelationReplaces, Incoming: true, Other: future},
			{Relation: LegalRelationAmends, Incoming: true, Other: &LegalDocument{EffectiveOn: day("2020-01-01")}},
			{Relation: LegalRelationReplaces, Incoming: true, Other: newLaw},
		},
	}
	by, on := LegalDerivedExpiry(rep, now)
	if by != newLaw || on == nil || !on.Equal(*day("2026-01-01")) {
		t.Fatalf("derived = %v %v, want the in-force replacement", by, on)
	}
	if LegalDerivedExpiryRelation(rep, by) != LegalRelationReplaces || LegalDerivedStatusText(rep.Document) != LegalStatusExpired {
		t.Fatal("relation / status text wrong")
	}
	rep.Document.Status = LegalStatusPartlyExpired
	if LegalDerivedStatusText(rep.Document) != LegalStatusMaybeExpired {
		t.Fatal("partly expired should only be maybe expired")
	}
	// an expiry date or an expired status is left as the source says
	rep.Document.ExpiredOn = day("2025-12-31")
	if by, _ := LegalDerivedExpiry(rep, now); by != nil {
		t.Fatal("a source expiry date wins")
	}
	rep.Document.ExpiredOn = nil
	rep.Document.Status = "Chưa có hiệu lực"
	if by, _ := LegalDerivedExpiry(rep, now); by != nil {
		t.Fatal("not-yet-in-force is not derivable")
	}
}
