package types

import (
	"encoding/json"
	"errors"
	"testing"
	"time"
)

// TestAbbreviationResolutionPublicStateProjection ensures the public DTO omits
// private evidence/owner/scope and does not alias internal slices.
func TestAbbreviationResolutionPublicStateProjection(t *testing.T) {
	r := AbbreviationResolution{
		RequestID:            "req-1",
		RootUserMessageID:    "root-1",
		CurrentUserMessageID: "cur-2",
		Version:              3,
		OriginalQuery:        "ATTT có yêu cầu gì",
		EffectiveQuery:       "An toàn thực phẩm (ATTT) có yêu cầu gì",
		Status:               "ready",
		UnknownTerms:         []string{"XYZ"},
		ExpiresAt:            time.Date(2026, 9, 26, 1, 0, 0, 0, time.UTC),
		Terms: []AbbreviationTerm{
			{
				ShortForm: "ATTT",
				Key:       "attt",
				Occurrences: []AbbreviationOccurrence{
					{Start: 0, End: 4},
				},
				Meanings: []AbbreviationMeaning{
					{ID: "m-1", FullForm: "An toàn thực phẩm", Description: "d1"},
				},
				SelectedID:       "m-1",
				FullForm:         "An toàn thực phẩm",
				Source:           "dictionary_active",
				Definition:       &AbbreviationDefinition{ShortForm: "ATTT", FullForm: "An toàn thực phẩm", SourceMessageID: "cur-2", Start: 0, End: 16},
				SuggestionID:     "s-1",
				SuggestionStatus: "pending_review",
			},
		},
	}
	pub := r.PublicState()

	if pub.RequestID != "req-1" || pub.RootUserMessageID != "root-1" {
		t.Fatalf("public ids not projected: %+v", pub)
	}
	if pub.Status != "ready" || pub.Version != 3 {
		t.Fatalf("public status/version not projected: %+v", pub)
	}
	if !pub.ExpiresAt.Equal(r.ExpiresAt) {
		t.Fatalf("public expiry not projected: %v", pub.ExpiresAt)
	}
	if len(pub.Terms) != 1 {
		t.Fatalf("expected 1 public term, got %d", len(pub.Terms))
	}
	pt := pub.Terms[0]
	if pt.ShortForm != "ATTT" || pt.FullForm != "An toàn thực phẩm" || pt.Source != "dictionary_active" {
		t.Fatalf("public term mismatch: %+v", pt)
	}
	if pt.SelectedID != "m-1" || pt.SuggestionStatus != "pending_review" {
		t.Fatalf("public selection/suggestion mismatch: %+v", pt)
	}
	if len(pub.UnknownTerms) != 1 || pub.UnknownTerms[0] != "XYZ" {
		t.Fatalf("public unknown terms mismatch: %v", pub.UnknownTerms)
	}

	// No aliasing: mutating internals after projection must not touch it.
	r.Terms[0].FullForm = "INTERNAL-MUT"
	r.Terms[0].Meanings[0].FullForm = "INTERNAL-MUT"
	r.UnknownTerms[0] = "INTERNAL-MUT"
	if pub.Terms[0].FullForm != "An toàn thực phẩm" {
		t.Fatalf("PublicState shares term storage with internal resolution: %q", pub.Terms[0].FullForm)
	}
	if pub.UnknownTerms[0] != "XYZ" {
		t.Fatalf("PublicState shares unknown storage with internal resolution: %q", pub.UnknownTerms[0])
	}
	// Mutating the projection must not touch the internals.
	pub.Terms[0].FullForm = "PROJECTION-MUT"
	pub.UnknownTerms[0] = "PROJECTION-MUT"
	if r.Terms[0].FullForm != "INTERNAL-MUT" {
		t.Fatalf("PublicState aliased internal term slice")
	}
	if r.UnknownTerms[0] != "INTERNAL-MUT" {
		t.Fatalf("PublicState aliased internal unknown slice")
	}
}

func TestAbbreviationResolutionErrors(t *testing.T) {
	errs := map[string]error{
		"not-ready":     ErrAbbreviationNotReady,
		"conflict":      ErrAbbreviationConflict,
		"not-found":     ErrAbbreviationNotFound,
		"bad-selection": ErrAbbreviationBadSelection,
	}
	seen := map[error]string{}
	for name, err := range errs {
		if err == nil {
			t.Fatalf("%s error var is nil", name)
		}
		if prev, dup := seen[err]; dup {
			t.Fatalf("%s and %s share one sentinel", name, prev)
		}
		seen[err] = name
	}
	if errors.Is(ErrAbbreviationNotReady, ErrAbbreviationConflict) {
		t.Fatalf("distinct sentinels must not match each other")
	}
	if MaxAbbreviationTerms != 100 {
		t.Fatalf("MaxAbbreviationTerms = %d, want 100", MaxAbbreviationTerms)
	}
}

// Persisted DTOs use explicit snake_case JSON tags and round-trip nested
// state; the public projection leaks no private evidence fields.
func TestAbbreviationResolutionSnakeCaseRoundTrip(t *testing.T) {
	r := AbbreviationResolution{
		RequestID:         "req-1",
		RootUserMessageID: "root-1",
		Version:           3,
		OriginalQuery:     "ATTT là gì",
		EffectiveQuery:    "An toàn thực phẩm (ATTT) là gì",
		Status:            "ready",
		Terms: []AbbreviationTerm{
			{
				ShortForm:   "ATTT",
				Key:         "attt",
				Occurrences: []AbbreviationOccurrence{{Start: 0, End: 4}},
				Meanings:    []AbbreviationMeaning{{ID: "m-1", FullForm: "An toàn thực phẩm", Description: "d1"}},
				SelectedID:  "m-1",
				FullForm:    "An toàn thực phẩm",
				Source:      "dictionary_active",
				Definition: &AbbreviationDefinition{
					ShortForm: "ATTT", FullForm: "An toàn thực phẩm",
					SourceMessageID: "msg-1", Start: 0, End: len("An toàn thực phẩm"),
				},
			},
		},
		UnknownTerms: []string{"ZZZQ"},
		ExpiresAt:    time.Date(2026, 9, 26, 1, 0, 0, 0, time.UTC),
	}
	raw, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("marshal err = %v", err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatalf("unmarshal err = %v", err)
	}
	for _, want := range []string{
		"request_id", "root_user_message_id", "version", "original_query",
		"effective_query", "status", "terms", "unknown_terms", "expires_at",
	} {
		if _, ok := fields[want]; !ok {
			t.Fatalf("missing snake_case field %q in %s", want, raw)
		}
	}
	for _, leak := range []string{"RequestID", "OriginalQuery", "Terms", "UnknownTerms"} {
		if _, ok := fields[leak]; ok {
			t.Fatalf("Go-case field %q leaked into JSON contract: %s", leak, raw)
		}
	}
	var back AbbreviationResolution
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatalf("round-trip err = %v", err)
	}
	if back.OriginalQuery != r.OriginalQuery || len(back.Terms) != 1 ||
		back.Terms[0].Occurrences[0] != (AbbreviationOccurrence{Start: 0, End: 4}) ||
		back.Terms[0].Definition == nil || back.Terms[0].Definition.End != len("An toàn thực phẩm") {
		t.Fatalf("round-trip mismatch: %+v", back)
	}
}

func TestAbbreviationPublicStateLeaksNoPrivateEvidence(t *testing.T) {
	r := AbbreviationResolution{
		RequestID:         "req-1",
		RootUserMessageID: "root-1",
		Status:            "ready",
		Version:           3,
		OriginalQuery:     "ATTT là gì",
		EffectiveQuery:    "An toàn thực phẩm (ATTT) là gì",
		Terms: []AbbreviationTerm{
			{
				ShortForm:   "ATTT",
				Key:         "attt",
				Occurrences: []AbbreviationOccurrence{{Start: 0, End: 4}},
				Meanings:    []AbbreviationMeaning{{ID: "m-1", FullForm: "An toàn thực phẩm"}},
				SelectedID:  "m-1",
				FullForm:    "An toàn thực phẩm",
				Source:      "dictionary_active",
				Definition: &AbbreviationDefinition{
					ShortForm: "ATTT", FullForm: "An toàn thực phẩm",
					SourceMessageID: "msg-1", Start: 0, End: len("An toàn thực phẩm"),
				},
			},
		},
		UnknownTerms: []string{"ZZZQ"},
		ExpiresAt:    time.Date(2026, 9, 26, 1, 0, 0, 0, time.UTC),
	}
	raw, err := json.Marshal(r.PublicState())
	if err != nil {
		t.Fatalf("marshal err = %v", err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatalf("unmarshal err = %v", err)
	}
	allowed := map[string]bool{
		"request_id": true, "root_user_message_id": true, "status": true,
		"version": true, "unknown_terms": true, "terms": true, "expires_at": true,
	}
	for name := range fields {
		if !allowed[name] {
			t.Fatalf("public projection leaked field %q: %s", name, raw)
		}
	}
	var terms []map[string]json.RawMessage
	if err := json.Unmarshal(fields["terms"], &terms); err != nil || len(terms) != 1 {
		t.Fatalf("public terms malformed: %s", raw)
	}
	for name := range terms[0] {
		switch name {
		case "short_form", "full_form", "source", "selected_id", "suggestion_status":
		default:
			t.Fatalf("public term leaked field %q: %s", name, raw)
		}
	}
}
