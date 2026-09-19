// Package chunker - vietnamese_legal.go implements the Vietnamese legal
// document tier: structure-aware splitting at Phần/Chương/Mục/Điều
// boundaries. Ported from AIRAG's LegalDocumentChunker
// (backend/app/services/embedding/chunker.py).
//
// A size-based splitter mixes the tail of one Điều with the head of the
// next, so retrieval by Điều/Khoản returns half-irrelevant content. This
// tier cuts at structural heading boundaries first (each Điều stays a
// complete unit, preamble/phụ lục preserved); only an oversized section is
// size-split — by Khoản boundaries first, falling back to the legacy
// splitter inside a single over-large Khoản.
//
// Metadata: after splitting, every chunk's ContextHeader carries its legal
// heading path ("Chương II > Điều 17. ...") and Chunk.Legal carries
// article_nos / khoan_nos / diem_labels / subdivision_refs so retrieval can
// resolve "Điều 17" exactly (no "Điều 3" matching "Điều 30").
package chunker

import (
	"strings"

	"github.com/Tencent/WeKnora/internal/vietnamese_legal"
)

// init wires this implementation into the strategy resolver.
func init() {
	splitByVietnameseLegal = splitByVietnameseLegalImpl
}

// splitByVietnameseLegalImpl is the Vietnamese-legal tier implementation.
// Returns nil when the document lacks legal structure (< MinDieuHeadings
// Điều headings) so the validator rejects the output and the chain falls
// through to the generic tiers — công văn, tờ trình and non-legal content
// keep the standard behaviour.
func splitByVietnameseLegalImpl(text string, cfg SplitterConfig, _ *DocProfile) []Chunk {
	if text == "" {
		return nil
	}
	headings := vietnamese_legal.FindHeadings(text)
	dieu := 0
	for _, h := range headings {
		if h.Level == vietnamese_legal.LevelDieu {
			dieu++
		}
	}
	if dieu < vietnamese_legal.MinDieuHeadings {
		return nil
	}

	runes := []rune(text)
	// Section boundaries: document start + every heading + document end.
	// The preamble (before the first heading) is its own section.
	bounds := []int{0}
	for _, h := range headings {
		if h.Start > 0 {
			bounds = append(bounds, h.Start)
		}
	}
	bounds = append(bounds, len(runes))

	var out []Chunk
	// Greedy packing: accumulate consecutive sections while the combined
	// size stays within ChunkSize. Vietnamese laws carry many tiny Điều
	// ("Điều 5. (Được bãi bỏ)"); keeping each one alone would trip the
	// validator's tiny-chunk rule and discard the whole tier. A merged
	// chunk stays contiguous in the source so Start/End slicing keeps the
	// End-Start == len(Content) invariant, and DeriveHeadingPaths lists
	// every Điều inside it so section lookup still hits any of them.
	packStart, packEnd := -1, -1
	flush := func() {
		if packStart >= 0 {
			out = append(out, Chunk{
				Content: string(runes[packStart:packEnd]),
				Seq:     len(out),
				Start:   packStart,
				End:     packEnd,
			})
			packStart, packEnd = -1, -1
		}
	}

	for i := 0; i+1 < len(bounds); i++ {
		s, e := bounds[i], bounds[i+1]
		if e <= s || strings.TrimSpace(string(runes[s:e])) == "" {
			continue
		}
		secLen := e - s
		switch {
		case secLen > cfg.ChunkSize:
			// Oversized section: emit the pending pack, then split this
			// section by Khoản (legacy-split only inside an oversized
			// single Khoản).
			flush()
			out = append(out, splitOversizedLegalSection(runes, s, e, cfg, len(out))...)
		case packStart < 0:
			packStart, packEnd = s, e
		case e-packStart <= cfg.ChunkSize:
			packEnd = e
		default:
			flush()
			packStart, packEnd = s, e
		}
	}
	flush()

	// Re-seq and attach legal metadata.
	for i := range out {
		out[i].Seq = i
	}
	attachLegalMetadata(out)
	return out
}

// splitOversizedLegalSection ports AIRAG's _split_oversized_section: cut a
// section longer than ChunkSize along Khoản boundaries first; a single
// Khoản (or the heading preamble) that alone exceeds ChunkSize falls back
// to the legacy splitter inside exactly that segment — the split never
// bleeds into the next Khoản/Điều.
func splitOversizedLegalSection(runes []rune, start, end int, cfg SplitterConfig, seq int) []Chunk {
	section := runes[start:end]
	subs := vietnamese_legal.FindSubdivisions(string(section))

	var khoans []int // rune offsets of accepted khoản markers, relative to section
	for _, sd := range subs {
		if sd.Kind == "khoan" {
			khoans = append(khoans, sd.Start)
		}
	}

	emit := func(relStart, relEnd int) []Chunk {
		sub := SplitText(string(section[relStart:relEnd]), cfg)
		out := make([]Chunk, 0, len(sub))
		for _, c := range sub {
			out = append(out, Chunk{
				Content: c.Content,
				Seq:     seq + len(out),
				Start:   start + relStart + c.Start,
				End:     start + relStart + c.End,
			})
		}
		return out
	}

	if len(khoans) == 0 {
		return emit(0, len(section))
	}

	// Segments: optional preamble (heading + intro before the first khoản),
	// then one segment per khoản.
	type seg struct{ s, e int }
	var segments []seg
	if strings.TrimSpace(string(section[:khoans[0]])) != "" {
		segments = append(segments, seg{0, khoans[0]})
	}
	for i, k := range khoans {
		e := len(section)
		if i+1 < len(khoans) {
			e = khoans[i+1]
		}
		segments = append(segments, seg{k, e})
	}

	var out []Chunk
	bufStart, bufEnd := -1, -1 // rune range of the open accumulation buffer
	flush := func() {
		if bufStart >= 0 {
			out = append(out, Chunk{
				Content: string(section[bufStart:bufEnd]),
				Seq:     seq + len(out),
				Start:   start + bufStart,
				End:     start + bufEnd,
			})
			bufStart, bufEnd = -1, -1
		}
	}
	for _, sg := range segments {
		switch {
		case sg.e-sg.s > cfg.ChunkSize:
			flush()
			out = append(out, emit(sg.s, sg.e)...)
		case bufStart < 0:
			bufStart, bufEnd = sg.s, sg.e
		case sg.e-bufStart <= cfg.ChunkSize:
			bufEnd = sg.e
		default:
			flush()
			bufStart, bufEnd = sg.s, sg.e
		}
	}
	flush()
	return out
}

// attachLegalMetadata derives per-chunk heading paths and Khoản/Điểm
// metadata. Chunk contents are fed in document order so the structural
// state carries across chunks that contain no header of their own (a
// Khoản-split continuation chunk still resolves to its open Điều).
func attachLegalMetadata(chunks []Chunk) {
	contents := make([]string, len(chunks))
	for i, c := range chunks {
		contents[i] = c.Content
	}
	paths := vietnamese_legal.DeriveHeadingPaths(contents)
	subMetas := vietnamese_legal.DeriveSubdivisionMetadata(contents, paths)

	for i := range chunks {
		path := paths[i]
		var legal *vietnamese_legal.LegalMetadata
		if len(path) > 0 || len(subMetas[i].KhoanNos) > 0 || len(subMetas[i].DiemLabels) > 0 {
			legal = &vietnamese_legal.LegalMetadata{
				HeadingPath:     path,
				ArticleNos:      vietnamese_legal.ExtractArticleNos(path),
				KhoanNos:        subMetas[i].KhoanNos,
				DiemLabels:      subMetas[i].DiemLabels,
				SubdivisionRefs: subMetas[i].SubdivisionRefs,
				SchemaVersion:   1,
			}
		}
		chunks[i].Legal = legal
		if len(path) > 0 {
			chunks[i].ContextHeader = strings.Join(path, " > ")
		}
	}
}
