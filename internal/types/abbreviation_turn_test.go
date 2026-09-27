package types

import (
	"encoding/json"
	"testing"
	"time"
)

func sampleTurnPayload() AbbreviationTurnPayload {
	return AbbreviationTurnPayload{
		Snapshot: AbbreviationRequestSnapshot{
			AgentID:          "agent-1",
			Mode:             "qa",
			AgentTenantID:    7,
			KnowledgeBaseIDs: []string{"kb-1"},
			KnowledgeIDs:     []string{"k-1"},
			MCPServiceIDs:    []string{"mcp-1"},
			SkillNames:       []string{"skill-1"},
			TagScopes:        []TagScope{{KnowledgeBaseID: "kb-1", TagIDs: []string{"t-1"}}},
			WebSearchEnabled: true,
			AttachmentIDs:    []string{"a-1"},
			Locale:           "vi",
		},
		Resolution: AbbreviationResolution{
			OriginalQuery: "ATTT là gì",
			Status:        "needs_definition",
			UnknownTerms:  []string{"ATTT"},
			Terms: []AbbreviationTerm{
				{ShortForm: "ATTT", Key: "attt", Occurrences: []AbbreviationOccurrence{{Start: 0, End: 4}}},
			},
		},
	}
}

func TestAbbreviationTurnTableNames(t *testing.T) {
	if (AbbreviationTurnState{}).TableName() != "abbreviation_turn_states" {
		t.Fatalf("state table = %q", (AbbreviationTurnState{}).TableName())
	}
	if (AbbreviationMessageLink{}).TableName() != "abbreviation_turn_messages" {
		t.Fatalf("link table = %q", (AbbreviationMessageLink{}).TableName())
	}
}

// The payload round-trips through the driver boundary with fresh slices:
// mutating the scanned copy must not alias the original.
func TestAbbreviationTurnPayloadValuerScanner(t *testing.T) {
	original := sampleTurnPayload()
	value, err := original.Value()
	if err != nil {
		t.Fatalf("Value err = %v", err)
	}
	raw, ok := value.(string)
	if !ok || raw == "" {
		t.Fatalf("Value = %#v, want non-empty JSON string", value)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &fields); err != nil {
		t.Fatalf("payload is not JSON: %v", err)
	}
	for _, want := range []string{"snapshot", "resolution"} {
		if _, ok := fields[want]; !ok {
			t.Fatalf("payload misses %q: %s", want, raw)
		}
	}

	var scanned AbbreviationTurnPayload
	if err := scanned.Scan([]byte(raw)); err != nil {
		t.Fatalf("Scan bytes err = %v", err)
	}
	var scannedStr AbbreviationTurnPayload
	if err := scannedStr.Scan(raw); err != nil {
		t.Fatalf("Scan string err = %v", err)
	}
	var scannedNil AbbreviationTurnPayload
	if err := scannedNil.Scan(nil); err != nil {
		t.Fatalf("Scan nil err = %v", err)
	}
	if len(scannedNil.Snapshot.KnowledgeBaseIDs) != 0 {
		t.Fatalf("Scan nil must reset, got %+v", scannedNil)
	}
	if err := scanned.Scan(42); err == nil {
		t.Fatalf("Scan of foreign type must fail")
	}
	var nilTarget *AbbreviationTurnPayload
	if err := nilTarget.Scan(raw); err == nil {
		t.Fatalf("Scan into nil target must fail")
	}

	if scanned.Snapshot.AgentID != "agent-1" ||
		len(scanned.Snapshot.TagScopes) != 1 || scanned.Snapshot.TagScopes[0].TagIDs[0] != "t-1" ||
		len(scanned.Resolution.UnknownTerms) != 1 || scanned.Resolution.UnknownTerms[0] != "ATTT" {
		t.Fatalf("round-trip mismatch: %+v", scanned)
	}
	scanned.Snapshot.KnowledgeBaseIDs[0] = "MUTATED"
	scanned.Snapshot.TagScopes[0].TagIDs[0] = "MUTATED"
	scanned.Resolution.UnknownTerms[0] = "MUTATED"
	scanned.Resolution.Terms[0].Occurrences[0].Start = 999
	if original.Snapshot.KnowledgeBaseIDs[0] != "kb-1" ||
		original.Snapshot.TagScopes[0].TagIDs[0] != "t-1" ||
		original.Resolution.UnknownTerms[0] != "ATTT" ||
		original.Resolution.Terms[0].Occurrences[0].Start != 0 {
		t.Fatalf("scanned payload aliases the original: %+v", original)
	}
}

func TestAbbreviationTurnStateJSONContract(t *testing.T) {
	state := AbbreviationTurnState{
		ID: "r", TenantID: 7, SessionID: "s", OwnerID: "alice",
		PrincipalID: "web_user:alice", RootUserMessageID: "u1",
		State: "inspecting", Version: 1, Payload: sampleTurnPayload(),
		CreatedAt: time.Date(2026, 9, 26, 1, 0, 0, 0, time.UTC),
		UpdatedAt: time.Date(2026, 9, 26, 1, 0, 0, 0, time.UTC),
		ExpiresAt: time.Date(2026, 9, 27, 1, 0, 0, 0, time.UTC),
	}
	raw, err := json.Marshal(state)
	if err != nil {
		t.Fatalf("marshal err = %v", err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatalf("unmarshal err = %v", err)
	}
	for _, want := range []string{
		"id", "tenant_id", "session_id", "owner_id", "principal_id",
		"root_user_message_id", "clarification_message_id",
		"executing_message_id", "state", "error_code", "version",
		"payload", "created_at", "updated_at", "expires_at",
	} {
		if _, ok := fields[want]; !ok {
			t.Fatalf("missing snake_case field %q in %s", want, raw)
		}
	}
}

// Scan decodes into a fresh value: absent fields, nulls and empty objects
// reset the payload instead of retaining previous data, malformed input
// leaves the receiver untouched, and decoded slices never alias.
func TestAbbreviationTurnPayloadScanFreshTarget(t *testing.T) {
	p := AbbreviationTurnPayload{
		Resolution: AbbreviationResolution{OriginalQuery: "old", UnknownTerms: []string{"OLD"}},
	}
	prior := p
	if err := p.Scan(`{"resolution":{"unknown_terms":["NEW"]}}`); err != nil {
		t.Fatalf("Scan err = %v", err)
	}
	if p.Resolution.OriginalQuery != "" {
		t.Fatalf("Scan retained absent fields: %+v", p.Resolution)
	}
	if prior.Resolution.UnknownTerms[0] != "OLD" {
		t.Fatalf("Scan reused backing array and mutated the prior payload")
	}
	if err := p.Scan(`null`); err != nil {
		t.Fatalf("Scan null err = %v", err)
	}
	if len(p.Resolution.UnknownTerms) != 0 || p.Snapshot.AgentID != "" {
		t.Fatalf("Scan null retained previous data: %+v", p)
	}
	if err := p.Scan(`{}`); err != nil {
		t.Fatalf("Scan empty object err = %v", err)
	}
	before := p
	if err := p.Scan(`{"resolution":`); err == nil {
		t.Fatalf("malformed Scan must fail")
	}
	if before.Resolution.UnknownTerms != nil || len(p.Resolution.UnknownTerms) != 0 {
		t.Fatalf("malformed Scan mutated the receiver: %+v", p)
	}
	if err := p.Scan(42); err == nil {
		t.Fatalf("foreign-type Scan must fail")
	}
}

// Nested definition evidence and large tenant IDs survive the driver
// boundary exactly.
func TestAbbreviationTurnPayloadScanNestedAndLargeIDs(t *testing.T) {
	original := AbbreviationTurnPayload{
		Snapshot: AbbreviationRequestSnapshot{AgentTenantID: 1<<63 - 1},
		Resolution: AbbreviationResolution{
			OriginalQuery: "ATTT là gì",
			Status:        "ready",
			Terms: []AbbreviationTerm{
				{
					ShortForm: "ATTT", Key: "attt",
					Meanings:   []AbbreviationMeaning{{ID: "m-1", FullForm: "An toàn thực phẩm"}},
					SelectedID: "m-1", FullForm: "An toàn thực phẩm",
					Source: "user_current_request",
					Definition: &AbbreviationDefinition{
						ShortForm: "ATTT", FullForm: "An toàn thực phẩm",
						SourceMessageID: "msg-1", Start: 0, End: len("An toàn thực phẩm"),
					},
				},
			},
		},
	}
	value, err := original.Value()
	if err != nil {
		t.Fatalf("Value err = %v", err)
	}
	var scanned AbbreviationTurnPayload
	if err := scanned.Scan(value); err != nil {
		t.Fatalf("Scan err = %v", err)
	}
	if scanned.Snapshot.AgentTenantID != 1<<63-1 {
		t.Fatalf("large tenant ID did not survive: %d", scanned.Snapshot.AgentTenantID)
	}
	def := scanned.Resolution.Terms[0].Definition
	if def == nil || def.SourceMessageID != "msg-1" || def.End != len("An toàn thực phẩm") {
		t.Fatalf("nested definition evidence did not survive: %+v", def)
	}
}

// Clone isolates every nested descendant for sibling query results.
func TestAbbreviationTurnPayloadClone(t *testing.T) {
	original := sampleTurnPayload()
	original.Resolution.Terms[0].Meanings = []AbbreviationMeaning{{ID: "m-1", FullForm: "F"}}
	original.Resolution.Terms[0].Definition = &AbbreviationDefinition{ShortForm: "ATTT", FullForm: "F"}
	cloned := original.Clone()
	cloned.Snapshot.KnowledgeBaseIDs[0] = "MUTATED"
	cloned.Snapshot.TagScopes[0].TagIDs[0] = "MUTATED"
	cloned.Resolution.UnknownTerms[0] = "MUTATED"
	cloned.Resolution.Terms[0].Occurrences[0].Start = 999
	cloned.Resolution.Terms[0].Meanings[0].ID = "MUTATED"
	cloned.Resolution.Terms[0].Definition.FullForm = "MUTATED"
	if original.Snapshot.KnowledgeBaseIDs[0] != "kb-1" ||
		original.Snapshot.TagScopes[0].TagIDs[0] != "t-1" ||
		original.Resolution.UnknownTerms[0] != "ATTT" ||
		original.Resolution.Terms[0].Occurrences[0].Start != 0 ||
		original.Resolution.Terms[0].Meanings[0].ID != "m-1" ||
		original.Resolution.Terms[0].Definition.FullForm != "F" {
		t.Fatalf("Clone aliases the original: %+v", original)
	}
}
