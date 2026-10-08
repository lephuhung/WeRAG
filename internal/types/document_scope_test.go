package types

import (
	"context"
	"testing"
)

func TestDocumentScopeNormalize(t *testing.T) {
	s := &DocumentScope{DocumentIDs: []string{" ws-1 ", "ws-1", "", "ws-2"}, Task: " Compare ", SetBy: DocumentScopeSetByUser,
		Sections: []DocumentScopeSection{{DocumentID: "ws-2", From: 3, To: 9, Title: "  Điều 3.   Tổ chức  "}}}
	if err := s.Normalize(); err != nil {
		t.Fatal(err)
	}
	if len(s.DocumentIDs) != 2 || s.DocumentIDs[0] != "ws-1" || s.Task != DocumentScopeTaskCompare || s.Sections[0].Title != "Điều 3. Tổ chức" {
		t.Fatalf("normalized: %+v", s)
	}
	if !s.Includes("ws-2") || s.Includes("ws-3") || len(s.SectionsOf("ws-2")) != 1 || len(s.SectionsOf("ws-1")) != 0 {
		t.Fatalf("lookups: %+v", s)
	}

	for name, bad := range map[string]*DocumentScope{
		"no document": {SetBy: DocumentScopeSetByUser},
		"bad task":    {DocumentIDs: []string{"a"}, Task: "dance", SetBy: DocumentScopeSetByUser},
		"no setter":   {DocumentIDs: []string{"a"}},
		"foreign section": {DocumentIDs: []string{"a"}, SetBy: DocumentScopeSetByRouter,
			Sections: []DocumentScopeSection{{DocumentID: "b", From: 1, To: 2}}},
		"reversed range": {DocumentIDs: []string{"a"}, SetBy: DocumentScopeSetByRouter,
			Sections: []DocumentScopeSection{{DocumentID: "a", From: 5, To: 2}}},
	} {
		if bad.Normalize() == nil {
			t.Errorf("%s: accepted %+v", name, bad)
		}
	}
}

func TestDocumentScopeContext(t *testing.T) {
	ctx := context.Background()
	if WithDocumentScope(ctx, nil) != ctx || DocumentScopeFromContext(ctx) != nil {
		t.Fatal("a nil scope leaves ctx alone")
	}
	s := &DocumentScope{DocumentIDs: []string{"a"}}
	if DocumentScopeFromContext(WithDocumentScope(ctx, s)) != s {
		t.Fatal("scope round trip")
	}
}
