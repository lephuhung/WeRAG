package service

import (
	"context"
	"encoding/json"
	"io"
	"strings"

	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/vbpl"
)

// vbplConverterVersionKey records, on the knowledge metadata, which
// internal/vbpl converter produced its chunks, so documents converted by an
// older version can be found and reparsed.
const vbplConverterVersionKey = "vbpl_converter_version"

// vbpl chunks hold one Điều each; the KB chunk size is honoured inside
// these bounds (the common 512 default would split most Điều by Khoản).
const (
	vbplMinChunkSize     = 1500
	vbplMaxChunkSize     = 4000
	vbplDefaultChunkSize = 2500
)

func vbplChunkSize(cc types.ChunkingConfig) int {
	switch n := cc.ChunkSize; {
	case n < vbplMinChunkSize:
		return vbplDefaultChunkSize
	case n > vbplMaxChunkSize:
		return vbplMaxChunkSize
	default:
		return n
	}
}

// processVBPLFile takes over the parse of an HTML page published by vbpl.vn
// (provision markup prov-article / prov-clause): the Go converter builds the
// Markdown and one chunk per Điều with Điều/Khoản/Điểm metadata and vbpl
// provision ids, bypassing docreader. It returns false — and the regular
// pipeline runs — for any other file, or when the page has no usable
// structure.
func (s *knowledgeService) processVBPLFile(
	ctx context.Context,
	payload types.DocumentProcessPayload,
	kb *types.KnowledgeBase,
	knowledge *types.Knowledge,
	eff types.EffectiveProcessConfig,
) bool {
	if payload.FilePath == "" {
		return false
	}
	switch strings.ToLower(strings.TrimPrefix(payload.FileType, ".")) {
	case "html", "htm":
	default:
		return false
	}
	rc, err := s.resolveFileServiceForPath(ctx, kb, payload.FilePath).GetFile(ctx, payload.FilePath)
	if err != nil {
		return false // convert() reports the read failure
	}
	raw, err := io.ReadAll(rc)
	rc.Close()
	if err != nil || !vbpl.LooksLikeVBPL(raw) {
		return false
	}
	doc, err := vbpl.ConvertBytes(raw)
	if err != nil || !doc.Structured {
		logger.Warnf(ctx, "[vbpl] knowledge %s: no usable structure (err=%v), using the regular parser", knowledge.ID, err)
		return false
	}

	s.beginStage(ctx, knowledge.ID, types.StageDocReader, types.JSONMap{
		"file_name": payload.FileName,
		"file_type": payload.FileType,
		"parser":    "vbpl",
	})
	planned := vbpl.Plan(doc, vbplChunkSize(eff.ChunkingConfig))
	sourceDocID := vbplSourceDocID(knowledge)
	appendixFiles := vbpl.LinkAttachedAppendices(planned, vbplAppendixFiles(knowledge))
	citations := vbpl.CitationLinks(doc, planned, sourceDocID)
	chunks := make([]types.ParsedChunk, len(planned))
	for i, c := range planned {
		legal := c.Legal()
		legal.SourceDocID = sourceDocID
		legal.AppendixFiles = appendixFiles[i]
		legal.Citations = citations[i]
		chunks[i] = types.ParsedChunk{
			Content:       c.Content,
			ContextHeader: c.ContextHeader(),
			Seq:           i,
			Start:         c.Start,
			End:           c.End,
			Legal:         legal,
		}
	}
	s.endStage(ctx, knowledge.ID, types.StageDocReader, types.JSONMap{
		"text_length":       len(doc.Markdown),
		"parser":            "vbpl",
		"converter_version": vbpl.ConverterVersion,
		"articles":          doc.Stats.Articles,
		"appendices":        doc.Stats.Appendices,
		"warnings":          doc.Warnings,
	})
	if eff.ChunkingConfig.EnableParentChild {
		logger.Infof(ctx, "[vbpl] knowledge %s: parent-child chunking ignored, one chunk per Điều", knowledge.ID)
	}
	s.recordVBPLConverterVersion(ctx, knowledge)
	logger.Infof(ctx, "[vbpl] knowledge %s: %d articles → %d chunks", knowledge.ID, doc.Stats.Articles, len(chunks))

	s.processChunks(ctx, kb, knowledge, chunks, ProcessChunksOptions{
		EnableQuestionGeneration: payload.EnableQuestionGeneration,
		QuestionCount:            payload.QuestionCount,
		EnableMultimodel:         payload.EnableMultimodel,
		Metadata:                 map[string]string{"parser": "vbpl"},
	})
	return true
}

func (s *knowledgeService) recordVBPLConverterVersion(ctx context.Context, knowledge *types.Knowledge) {
	meta := map[string]interface{}{}
	if len(knowledge.Metadata) > 0 {
		if m, err := knowledge.Metadata.Map(); err == nil && m != nil {
			meta = m
		}
	}
	meta[vbplConverterVersionKey] = vbpl.ConverterVersion
	b, err := json.Marshal(meta)
	if err != nil {
		return
	}
	knowledge.Metadata = types.JSON(b)
	if err := s.repo.UpdateKnowledgeColumn(ctx, knowledge.ID, "metadata", knowledge.Metadata); err != nil {
		logger.Warnf(ctx, "[vbpl] knowledge %s: record converter version: %v", knowledge.ID, err)
	}
}

// vbplSourceDocID is the vbpl.vn document id of the knowledge: its
// external_id, as set by scripts/legal_catalog/ingest_vbpl.py. Citations
// deep-link to the vbpl.vn page with it.
func vbplSourceDocID(knowledge *types.Knowledge) string {
	if len(knowledge.Metadata) == 0 {
		return ""
	}
	m, err := knowledge.Metadata.Map()
	if err != nil {
		return ""
	}
	id, _ := m["external_id"].(string)
	return strings.TrimSpace(id)
}

// vbplAppendixFilesKey holds, on the knowledge metadata, the phụ lục files
// vbpl.vn attaches to the document (set by ingest_vbpl.py, JSON-encoded
// since upload metadata values are strings): [{"title", "file_name"}].
// Chunks that refer to a phụ lục not in the page text link its file.
const vbplAppendixFilesKey = "vbpl_appendix_files"

func vbplAppendixFiles(knowledge *types.Knowledge) []vbpl.AttachedFile {
	if len(knowledge.Metadata) == 0 {
		return nil
	}
	m, err := knowledge.Metadata.Map()
	if err != nil {
		return nil
	}
	var raw []byte
	switch v := m[vbplAppendixFilesKey].(type) {
	case string:
		raw = []byte(v)
	case []interface{}:
		raw, _ = json.Marshal(v)
	default:
		return nil
	}
	var files []vbpl.AttachedFile
	if err := json.Unmarshal(raw, &files); err != nil {
		return nil
	}
	return files
}

// vbplSkipsQuestionGeneration reports whether the knowledge was ingested from
// vbpl.vn (metadata source "vbpl", set by ingest_vbpl.py). Its chunks are
// whole Điều with provision metadata, so generated questions add little
// while costing one LLM call per chunk across ~200k chunks: they are never
// generated for it, even when the locked parse defaults turn them on.
func vbplSkipsQuestionGeneration(knowledge *types.Knowledge) bool {
	if knowledge == nil || len(knowledge.Metadata) == 0 {
		return false
	}
	m, err := knowledge.Metadata.Map()
	if err != nil {
		return false
	}
	source, _ := m["source"].(string)
	return source == "vbpl"
}
