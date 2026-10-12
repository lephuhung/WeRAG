package service

import (
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
)

func TestVBPLChunkSize(t *testing.T) {
	for _, tc := range []struct{ in, want int }{
		{0, vbplDefaultChunkSize},
		{512, vbplDefaultChunkSize},
		{2000, 2000},
		{9000, vbplMaxChunkSize},
	} {
		if got := vbplChunkSize(types.ChunkingConfig{ChunkSize: tc.in}); got != tc.want {
			t.Errorf("vbplChunkSize(%d) = %d, want %d", tc.in, got, tc.want)
		}
	}
}

func TestVBPLAppendixFiles(t *testing.T) {
	encoded := `{"external_id":"x","vbpl_appendix_files":"[{\"title\":\"Phụ lục II\",\"file_name\":\"pl2.docx\"}]"}`
	got := vbplAppendixFiles(&types.Knowledge{Metadata: types.JSON(encoded)})
	if len(got) != 1 || got[0].Title != "Phụ lục II" || got[0].FileName != "pl2.docx" {
		t.Fatalf("JSON-encoded list: %+v", got)
	}
	list := `{"vbpl_appendix_files":[{"title":"Phụ lục I","file_name":"pl1.doc"}]}`
	if got := vbplAppendixFiles(&types.Knowledge{Metadata: types.JSON(list)}); len(got) != 1 || got[0].FileName != "pl1.doc" {
		t.Fatalf("plain list: %+v", got)
	}
	if got := vbplAppendixFiles(&types.Knowledge{Metadata: types.JSON(`{"external_id":"x"}`)}); got != nil {
		t.Fatalf("no files: %+v", got)
	}
}
