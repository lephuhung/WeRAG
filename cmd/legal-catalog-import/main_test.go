package main

import (
	"context"
	"sort"
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/google/uuid"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
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

// An edge at a twin of a placeholder goes to the full copy that replaced
// the placeholder.
func TestEdgeBuilderAliasChain(t *testing.T) {
	ids := map[string]bool{"twin": true, "full": true, "500": true}
	known := map[string]bool{"twin": true, "full": true, "500": true, "placeholder": true}
	b := newEdgeBuilder(ids, known, map[string]string{"twin": "placeholder", "placeholder": "full"})
	b.add(vbplRelation{DocID: "500", OtherDocID: "twin", Relationship: "Thay thế"})
	if len(b.rels) != 1 || b.rels[0].ToID != "full" {
		t.Fatalf("rels = %+v", b.rels)
	}
}

// Fable LOW: a -merge import whose full copy replaces a catalogued
// placeholder moves the placeholder's stored edges to the full copy, so
// no edge is left under two ids.
func TestMergeRelationsMovesTwinEdges(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&types.LegalDocumentRelation{}); err != nil {
		t.Fatal(err)
	}
	rel := func(from, to string, r types.LegalRelation) *types.LegalDocumentRelation {
		return &types.LegalDocumentRelation{FromID: from, ToID: to, Relation: r}
	}
	// stored: the placeholder's edges and one unrelated edge
	require := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	require(db.Create([]*types.LegalDocumentRelation{
		rel("500", "placeholder", types.LegalRelationReplaces), // also in the import, at the full copy
		rel("placeholder", "600", types.LegalRelationBasedOn),  // only stored
		rel("placeholder", "full", types.LegalRelationBasedOn), // becomes a self edge
		rel("700", "800", types.LegalRelationBasedOn),          // untouched
	}).Error)
	ids := map[string]bool{"full": true, "500": true}
	alias := map[string]string{"placeholder": "full"}
	b := newEdgeBuilder(ids, map[string]bool{"full": true, "500": true, "placeholder": true, "600": true}, alias)
	b.add(vbplRelation{DocID: "500", OtherDocID: "full", Relationship: "Thay thế"})
	require(mergeRelations(context.Background(), db, ids, alias, b.survivor, b.rels))

	var got []*types.LegalDocumentRelation
	require(db.Find(&got).Error)
	var edges []string
	for _, r := range got {
		edges = append(edges, r.FromID+" "+string(r.Relation)+" "+r.ToID)
	}
	sort.Strings(edges)
	want := []string{
		"500 " + string(types.LegalRelationReplaces) + " full",
		"700 " + string(types.LegalRelationBasedOn) + " 800",
		"full " + string(types.LegalRelationBasedOn) + " 600",
	}
	if strings.Join(edges, "|") != strings.Join(want, "|") {
		t.Errorf("edges = %v, want %v", edges, want)
	}
}

func TestFeedAdoptions(t *testing.T) {
	cat := []*types.LegalDocument{
		{ID: "tvpl-1", Source: "tvpl"}, {ID: "tvpl-2", Source: "tvpl"}, {ID: "tvpl-3", Source: "tvpl"},
		{ID: "ph", Source: "vbpl"},
	}
	alias := map[string]string{
		"tvpl-1": "v1",                 // one feed row → adopted
		"tvpl-2": "v2", "tvpl-3": "v2", // two feed rows of one portal row → neither
		"ph": "v4", // a portal placeholder: not a feed row
	}
	got := feedAdoptions(cat, alias)
	if len(got) != 1 || got["tvpl-1"] != "v1" {
		t.Fatalf("feedAdoptions = %v, want only tvpl-1 → v1", got)
	}
}
