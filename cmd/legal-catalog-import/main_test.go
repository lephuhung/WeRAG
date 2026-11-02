package main

import (
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
)

// TestEdgeBuilderTwins: an edge at a twin goes to its surviving row, the
// copy both twins list is kept once, and an edge between a twin and its
// survivor (now a self edge) is dropped.
func TestEdgeBuilderTwins(t *testing.T) {
	ids := map[string]bool{"22897": true, "109088": true, "500": true, "600": true}
	known := map[string]bool{"22897": true, "109088": true, "500": true, "600": true, "700": true}
	b := newEdgeBuilder(ids, known, map[string]string{"109088": "22897"})
	for _, r := range []vbplRelation{
		{DocID: "500", OtherDocID: "22897", Relationship: "Thay thế"},
		{DocID: "500", OtherDocID: "109088", Relationship: "Thay thế"}, // same edge via the twin
		{DocID: "109088", OtherDocID: "600", Relationship: "Căn cứ"},   // re-pointed
		{DocID: "109088", OtherDocID: "22897", Relationship: "Văn bản liên quan khác"},
		{DocID: "109088", OtherDocID: "999", Relationship: "Căn cứ"}, // unknown end
		{DocID: "700", OtherDocID: "800", Relationship: "Căn cứ"},    // touches no imported row
		{DocID: "500", OtherDocID: "600", Relationship: "Bản dịch"},
	} {
		b.add(r)
	}
	got := map[string]bool{}
	for _, r := range b.rels {
		got[r.FromID+" "+string(r.Relation)+" "+r.ToID] = true
	}
	want := []string{
		"500 " + string(types.LegalRelationReplaces) + " 22897",
		"22897 " + string(types.LegalRelationBasedOn) + " 600",
	}
	if len(got) != len(want) {
		t.Fatalf("edges = %v, want %v", got, want)
	}
	for _, w := range want {
		if !got[w] {
			t.Errorf("missing edge %q in %v", w, got)
		}
	}
	if b.skipped["outside catalogue"] != 3 || b.skipped["label:Bản dịch"] != 1 {
		t.Errorf("skipped = %v", b.skipped)
	}
}

// TestPruneCandidatesKeepsSurvivors: a -prune import deletes the vbpl rows
// absent from it, but not a reviewed row nor the surviving row of a twin
// it imported (the document is in the import under the twin's id).
func TestPruneCandidatesKeepsSurvivors(t *testing.T) {
	ids := map[string]bool{"200001": true, "500": true}
	alias := map[string]string{"200001": "22897", "absent-twin": "61017"}
	reviewed := map[string]*types.LegalDocument{"777": {ID: "777"}}
	got := pruneCandidates([]string{"22897", "61017", "500", "777", "888"}, ids, alias, reviewed)
	want := []string{"61017", "888"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("pruneCandidates = %v, want %v", got, want)
	}
}
