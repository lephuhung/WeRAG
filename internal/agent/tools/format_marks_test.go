package tools

import (
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/docformat"
	"github.com/Tencent/WeKnora/internal/docformat/docxedit"
)

func strayCharsFixture(t *testing.T) []byte {
	t.Helper()
	b, err := os.ReadFile("../../docformat/testdata/parity/fx_stray_chars.docx")
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func markOps(t *testing.T, data map[string]interface{}) []DocumentOp {
	t.Helper()
	all, _ := data["document_ops"].([]DocumentOp)
	var marks []DocumentOp
	for _, op := range all {
		if op.Op == OpMark {
			marks = append(marks, op)
		}
	}
	return marks
}

func TestFormatMarkOps(t *testing.T) {
	vdoc := newVirtualDoc([]docxedit.Paragraph{
		{Text: "CỘNG HÒA XÃ HỘI CHỦ NGHĨA VIỆT NAM"},
		{Text: ""},
		{Text: "Chính p`hủ ban hành"},
		{Text: "Nội dung"},
	})
	ops := formatMarkOps(vdoc, []formatSpot{
		{Para: 2, Text: "p`h"}, // a quoted typo: underlined
		{Para: 2, Text: "p`h"}, // the same one again: once
		{Para: 3},              // a paragraph-level finding: the whole paragraph
		{Para: 3},              // twice for the same paragraph: once
		{Para: 1},              // empty paragraph: skipped
		{Para: 9},              // outside the document: skipped
		{Para: 0, Text: "không có"},
	})
	if len(ops) != 3 {
		t.Fatalf("ops: %+v", ops)
	}
	if ops[0].Style != "underline" || ops[0].Text != "p`h" || ops[0].Anchor.Text != "Chính p`hủ ban hành" {
		t.Fatalf("typo mark: %+v", ops[0])
	}
	if ops[1].Style != "underline" || ops[1].Text != "" || ops[1].Anchor.Text != "Nội dung" {
		t.Fatalf("paragraph mark: %+v", ops[1])
	}
	// quoted text not in the paragraph: the whole paragraph is marked
	if ops[2].Style != "underline" || ops[2].Text != "" || ops[2].Anchor.Text != "CỘNG HÒA XÃ HỘI CHỦ NGHĨA VIỆT NAM" {
		t.Fatalf("fallback mark: %+v", ops[2])
	}

	many := make([]formatSpot, 0, 100)
	paras := make([]docxedit.Paragraph, 100)
	for i := range paras {
		paras[i].Text = "đoạn " + strings.Repeat("x", i+1)
		many = append(many, formatSpot{Para: i})
	}
	if got := formatMarkOps(newVirtualDoc(paras), many); len(got) != maxFormatMarks {
		t.Fatalf("capped at %d, got %d", maxFormatMarks, len(got))
	}
}

func TestCheckSpotsReadsEvidence(t *testing.T) {
	c := docformat.CheckResult{ID: "text.stray_chars", Status: docformat.StatusFail, Evidence: []map[string]any{
		{"para": 4, "text": "Chính p`hủ", "actual": "p`h"},
		{"para": float64(7), "text": "x", "actual": "y"}, // after a JSON round trip
		{"text": "no paragraph"},
	}}
	got := checkSpots(c, "stray_chars")
	if len(got) != 2 || got[0] != (formatSpot{Para: 4, Text: "p`h"}) || got[1].Para != 7 {
		t.Fatalf("spots: %+v", got)
	}
	// a property rule: "actual" is a value, not text in the paragraph
	prop := docformat.CheckResult{Evidence: []map[string]any{{"para": 2, "actual": "Arial"}}}
	if got := checkSpots(prop, "prop"); len(got) != 1 || got[0].Text != "" {
		t.Fatalf("prop spots: %+v", got)
	}
}

func TestCheckDocumentFormatMarksFindingsInTheEditor(t *testing.T) {
	// the body is in Arial: noi_dung.font fails on its paragraphs
	ws := newFakeWorkspace(docxFixture(t))
	res := runFormatTool(t, NewCheckDocumentFormatToolForWorkspace(ws, nil, "sess-9"), `{}`)
	if !res.Success {
		t.Fatalf("result: %+v", res)
	}
	marks := markOps(t, res.Data)
	if len(marks) == 0 || res.Data["ops_batch_id"] == nil || res.Data["document_id"] == nil {
		t.Fatalf("marks: %+v data: %+v", marks, res.Data)
	}
	for _, m := range marks {
		// a red underline only: the assistant changes neither text nor color
		if m.Style != "underline" || m.Anchor == nil || m.Anchor.Text == "" {
			t.Fatalf("mark: %+v", m)
		}
	}
	if len(ws.snapshots) != 1 || ws.snapshots[0] != "ai: đánh dấu lỗi thể thức" {
		t.Fatalf("an undo point before marking: %v", ws.snapshots)
	}
	if !strings.Contains(res.Output, "gạch chân đỏ") {
		t.Fatalf("output: %s", res.Output)
	}

	// mark=false: the check only reads
	ws = newFakeWorkspace(docxFixture(t))
	res = runFormatTool(t, NewCheckDocumentFormatToolForWorkspace(ws, nil, "sess-9"), `{"mark":false}`)
	if !res.Success || res.Data["document_ops"] != nil || len(ws.snapshots) != 0 {
		t.Fatalf("mark=false: %+v snapshots %v", res.Data, ws.snapshots)
	}
}

func TestCheckDocumentFormatUnderlinesStrayCharacters(t *testing.T) {
	ws := newFakeWorkspace(strayCharsFixture(t))
	res := runFormatTool(t, NewCheckDocumentFormatToolForWorkspace(ws, nil, "sess-9"), `{}`)
	if !res.Success {
		t.Fatalf("result: %+v", res)
	}
	underlined := 0
	for _, m := range markOps(t, res.Data) {
		if m.Style == "underline" && m.Text != "" {
			underlined++
			if !strings.Contains(m.Anchor.Text, m.Text) {
				t.Fatalf("typo mark outside its paragraph: %+v", m)
			}
		}
	}
	if underlined == 0 {
		t.Fatalf("no stray character underlined: %+v", res.Data["document_ops"])
	}
}

func TestApplyFormatFixesMarksWhatIsLeftForTheUser(t *testing.T) {
	// a stray character cannot be fixed mechanically: it is listed and
	// underlined where it is
	ws := newFakeWorkspace(strayCharsFixture(t))
	res := runTool(t, NewApplyFormatFixesTool(ws, nil, "s"), `{"apply":true,"force":true}`)
	if !res.Success {
		t.Fatalf("result: %+v", res)
	}
	manual, _ := res.Data["manual"].([]ManualFormatFix)
	var stray *ManualFormatFix
	for i := range manual {
		if manual[i].CheckID == "text.stray_chars" {
			stray = &manual[i]
		}
	}
	if stray == nil || len(stray.Paragraphs) == 0 {
		t.Fatalf("manual: %+v", manual)
	}
	marked := false
	for _, m := range markOps(t, res.Data) {
		if m.Style == "underline" && m.Text != "" {
			marked = true
		}
	}
	if !marked || !strings.Contains(res.Output, "gạch chân đỏ") {
		t.Fatalf("marks: %+v output: %s", res.Data["document_ops"], res.Output)
	}

	// a dry run only lists
	ws = newFakeWorkspace(strayCharsFixture(t))
	res = runTool(t, NewApplyFormatFixesTool(ws, nil, "s"), `{"dry_run":true,"force":true}`)
	if len(markOps(t, res.Data)) != 0 {
		t.Fatalf("dry run marked: %+v", res.Data["document_ops"])
	}
}

func TestApplyFormatFixesReviewsBeforeChanging(t *testing.T) {
	// the default call points out: no formatting change, only red
	// underlines on the paragraphs the plan would change
	ws := newFakeWorkspace(docxFixture(t))
	res := runTool(t, NewApplyFormatFixesTool(ws, nil, "s"), `{}`)
	if !res.Success || res.Data["review"] != true || res.Data["dry_run"] != true {
		t.Fatalf("data: %+v", res.Data)
	}
	all, _ := res.Data["document_ops"].([]DocumentOp)
	if len(all) == 0 {
		t.Fatal("a review underlines where the fixes would go")
	}
	for _, op := range all {
		if op.Op != OpMark || op.Style != "underline" {
			t.Fatalf("a review changes nothing but underlines: %+v", op)
		}
	}
	if len(ws.snapshots) != 1 || ws.snapshots[0] != "ai: đánh dấu lỗi thể thức" {
		t.Fatalf("snapshots: %v", ws.snapshots)
	}
	if !strings.Contains(res.Output, "CHƯA SỬA GÌ") || !strings.Contains(res.Output, "apply=true") {
		t.Fatalf("output: %s", res.Output)
	}

	// apply=true after the user agreed: the formatting is changed
	ws = newFakeWorkspace(docxFixture(t))
	res = runTool(t, NewApplyFormatFixesTool(ws, nil, "s"), `{"apply":true}`)
	applied := false
	all, _ = res.Data["document_ops"].([]DocumentOp)
	for _, op := range all {
		if op.Op == OpFormatParagraph {
			applied = true
		}
	}
	if !applied || res.Data["review"] != false {
		t.Fatalf("apply: %+v", res.Data)
	}
}

func TestCheckDocumentFormatUnderlinesSpellingMistakes(t *testing.T) {
	// the spelling pass runs with the check: its mistakes are underlined
	// in red however the model then words the answer
	ws := newFakeWorkspace(spellDoc(t))
	speller := &spellChat{replies: []string{spellReply}}
	tool := NewCheckDocumentFormatToolForWorkspace(ws, nil, "sess-9").WithSpelling(NewCheckSpellingTool(ws, speller, "sess-9"))
	if !strings.Contains(tool.Description(), "reviews the spelling") {
		t.Fatal("the description tells the model the spelling is covered")
	}
	res := runFormatTool(t, tool, `{}`)
	if !res.Success || speller.calls != 1 {
		t.Fatalf("result: %+v calls=%d", res, speller.calls)
	}
	found, _ := res.Data["spelling"].([]SpellingFinding)
	if len(found) != 4 {
		t.Fatalf("spelling: %+v", res.Data["spelling"])
	}
	underlined := map[string]bool{}
	for _, m := range markOps(t, res.Data) {
		if m.Style == "underline" && m.Text != "" {
			underlined[m.Text] = true
		}
	}
	for _, w := range []string{"đề nghi", "triễn khai", "p`hủ"} {
		if !underlined[w] {
			t.Errorf("%q not underlined: %+v", w, res.Data["document_ops"])
		}
	}
	if !strings.Contains(res.Output, "## Lỗi chính tả (4 lỗi, đã gạch chân đỏ") || !strings.Contains(res.Output, "“đề nghi” → “đề nghị”") {
		t.Fatalf("output: %s", res.Output)
	}
	if len(ws.snapshots) != 1 {
		t.Fatalf("one undo point for all marks: %v", ws.snapshots)
	}

	// mark=false: nothing is reviewed or marked
	ws = newFakeWorkspace(spellDoc(t))
	speller = &spellChat{replies: []string{spellReply}}
	tool = NewCheckDocumentFormatToolForWorkspace(ws, nil, "sess-9").WithSpelling(NewCheckSpellingTool(ws, speller, "sess-9"))
	if res := runFormatTool(t, tool, `{"mark":false}`); res.Data["spelling"] != nil || speller.calls != 0 {
		t.Fatalf("mark=false: %+v calls=%d", res.Data, speller.calls)
	}

	// the spelling model does not answer: said so, nothing invented
	ws = newFakeWorkspace(spellDoc(t))
	down := &spellChat{err: errors.New("down")}
	tool = NewCheckDocumentFormatToolForWorkspace(ws, nil, "sess-9").WithSpelling(NewCheckSpellingTool(ws, down, "sess-9"))
	res = runFormatTool(t, tool, `{}`)
	// the stray character is still found by code, so the pass reports it
	if !res.Success || !strings.Contains(res.Output, "## Lỗi chính tả") {
		t.Fatalf("model down: %s", res.Output)
	}
}
