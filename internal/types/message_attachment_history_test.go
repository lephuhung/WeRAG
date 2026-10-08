package types

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestBuildPromptOmittedAttachmentShowsMetadataAndNote(t *testing.T) {
	atts := MessageAttachments{{
		FileName: "a.pdf", FileType: ".pdf", FileSize: 1024, ContentMode: "full",
		HistoryOmission: &AttachmentHistoryOmission{Handle: "vb3"},
	}}
	got := atts.BuildPrompt()
	for _, want := range []string{`name="a.pdf"`, "<type>.pdf</type>", "<content_mode>full</content_mode>",
		"<status>omitted_in_history</status>", "tài liệu nguồn vb3"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in\n%s", want, got)
		}
	}
	if strings.Contains(got, "extraction failed") || strings.Contains(got, "<content>") {
		t.Fatalf("omitted attachment rendered as empty or with content:\n%s", got)
	}
	// the marker is never stored
	raw, _ := json.Marshal(atts[0])
	if strings.Contains(string(raw), "vb3") || strings.Contains(string(raw), "omission") {
		t.Fatalf("stored form carries the marker: %s", raw)
	}
}

func TestAttachmentHistoryOmissionNotes(t *testing.T) {
	cases := []struct {
		o    *AttachmentHistoryOmission
		want string
	}{
		{&AttachmentHistoryOmission{}, "tài liệu nguồn vbN) / read_document_outline"},
		{&AttachmentHistoryOmission{Handle: "vb2"}, "find_in_documents (document=vb2)"},
		{&AttachmentHistoryOmission{Handle: "vb1", Target: true}, "tab soạn thảo vb1"},
	}
	for _, c := range cases {
		if got := c.o.Note(); !strings.Contains(got, c.want) {
			t.Fatalf("note %q lacks %q", got, c.want)
		}
	}
}
