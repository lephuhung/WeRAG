package service

import "testing"

func TestWithFinalMessageCopies(t *testing.T) {
	data := map[string]interface{}{"display_type": "legal_document_request"}
	got := withFinalMessage(data, "Nghị định … đã hết hiệu lực.")
	if got["final_message"] != "Nghị định … đã hết hiệu lực." || got["display_type"] != "legal_document_request" {
		t.Fatalf("got %v", got)
	}
	if _, changed := data["final_message"]; changed {
		t.Fatal("the card's own data must not change")
	}
	if _, ok := withFinalMessage(nil, " ")["final_message"]; ok {
		t.Fatal("no final_message for an empty answer")
	}
}
