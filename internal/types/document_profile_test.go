package types

import (
	"strings"
	"testing"
)

func TestDocumentProfilePublicHidesTheHashAndFlagsAnEdit(t *testing.T) {
	ws := &DocumentWorkspace{ID: "ws-1", Revision: 2, SaveCount: 5}
	p := &DocumentProfile{Status: DocumentProfileReady, TextHash: "abc", Role: DocumentWorkspaceRoleTarget, Revision: 2, SaveCount: 5}
	if got := p.Public(ws); got.TextHash != "" || got.Stale || p.TextHash != "abc" {
		t.Fatalf("public copy: %+v (original hash %q)", got, p.TextHash)
	}
	ws.SaveCount = 6
	if !p.Public(ws).Stale {
		t.Fatal("a save after the profile makes it stale")
	}
	ws.Role, ws.SaveCount = DocumentWorkspaceRoleSource, 5
	if p.Describes(ws) {
		t.Fatal("a profile of the target does not describe the demoted source")
	}
}

func TestDocumentProfileNormalizeBoundsTheModelFields(t *testing.T) {
	p := (&DocumentProfile{
		DocType: "KẾ HOẠCH", DocumentNumber: "Số: 45 / KH-UBND",
		KeyPoints:        []string{"a", "A", "b", "c", "d", "e", "f"},
		TypicalQuestions: []string{" q1 ", "q2", "q3", "q4"},
		Gist:             strings.Repeat("x", 500),
		Entities:         &DocumentProfileEntities{},
	}).Normalize()
	if p.DocType != "Kế hoạch" || p.DocTypeCode != "ke_hoach" || p.DocumentNumber != "45/KH-UBND" {
		t.Fatalf("identity: %+v", p)
	}
	if len(p.KeyPoints) != DocumentProfileMaxKeyPoints || len(p.TypicalQuestions) != DocumentProfileMaxQuestions || p.Entities != nil {
		t.Fatalf("bounds: %+v", p)
	}
	if n := len([]rune(p.Gist)); n > 300 {
		t.Fatalf("gist is %d runes", n)
	}
}
