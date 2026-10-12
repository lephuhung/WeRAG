package vbpl

import (
	"reflect"
	"strings"
	"testing"
)

func TestLinkAttachedAppendices(t *testing.T) {
	files := []AttachedFile{
		{Title: "Thông-tư-148-2026-TT-BTC. Phụ lục I Nội dung CTTK Tỉnh", FileName: "pl1.doc"},
		{Title: "Thông-tư-148-2026-TT-BTC.Phụ lục II.Nội dung HTCT Xã", FileName: "pl2.docx"},
		{Title: "", FileName: "Phụ lục 3. Biểu mẫu.zip"},
	}
	inlineIV := strings.Repeat("Danh mục chỉ tiêu. ", 30)
	chunks := []Chunk{
		{Content: "1. Nội dung theo Phụ lục II ban hành kèm theo Thông tư này."},
		{Content: "Các chỉ tiêu tại Phụ lục I, II và III kèm theo Thông tư này."},
		{Content: "theo mẫu số 03 Phụ lục II ban hành kèm theo Thông tư số 38/2015/TT-BTC"},
		{Content: "Điều 2. Thay thế Phụ lục I ban hành kèm theo Thông tư số 49/2015/TT-BGTVT"},
		{Content: "Danh mục tại Phụ lục IV ban hành kèm theo Thông tư này."},
		{Content: "Phụ lục kèm theo Thông tư này."},
		{Content: "PHỤ LỤC IV\n" + inlineIV, Appendix: "PHỤ LỤC IV DANH MỤC"},
		{Content: "PHỤ LỤC II\n(Kèm theo Thông tư số 148/2026/TT-BTC)", Appendix: "PHỤ LỤC II NỘI DUNG"},
	}
	got := LinkAttachedAppendices(chunks, files)
	want := [][]string{
		{"pl2.docx"},
		{"pl1.doc", "pl2.docx", "Phụ lục 3. Biểu mẫu.zip"},
		nil,          // another document's phụ lục
		{"pl1.doc"},  // a replacement this document attaches
		nil,          // Phụ lục IV is in the page text
		nil,          // unnumbered while the page holds a phụ lục
		nil,          // inside the inline Phụ lục IV
		{"pl2.docx"}, // a heading-only phụ lục links its file
	}
	for i := range chunks {
		var names []string
		for _, f := range got[i] {
			names = append(names, f.FileName)
		}
		if !reflect.DeepEqual(names, want[i]) {
			t.Errorf("chunk %d: linked %v, want %v", i, names, want[i])
		}
	}
	if l := got[0][0]; l.Label != "II" || l.Name() != "Thông-tư-148-2026-TT-BTC.Phụ lục II.Nội dung HTCT Xã" {
		t.Errorf("file = %+v", l)
	}
	if l := got[1][2]; l.Label != "3" || l.Name() != "Phụ lục 3. Biểu mẫu" {
		t.Errorf("untitled file = %+v, name %q", l, l.Name())
	}
}

func TestAppendixKey(t *testing.T) {
	for in, want := range map[string]string{"II": "2", "2": "2", "02": "2", "IV": "4", "XIV": "14", "3a": "3a"} {
		if got := appendixKey(in); got != want {
			t.Errorf("appendixKey(%q) = %q, want %q", in, got, want)
		}
	}
}
