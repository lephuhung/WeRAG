package session

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/google/uuid"
)

// ToolImagePersister persists tool-produced binary images (e.g. MCP
// text-to-image results) as message artifacts so the chat panel can render
// and download them through the existing artifact pipeline.
//
// It is intentionally narrow: fileService.SaveBytes + catalog binding are the
// only dependencies. The zero value (nil fileService) degrades to "persist
// nothing" so turns without storage still complete.
type ToolImagePersister struct {
	fileService interfaces.FileService
	catalog     interfaces.ResourceCatalog
}

// NewToolImagePersister wires a ToolImagePersister. Either argument may be
// nil: a nil fileService persists nothing, a nil catalog skips binding.
func NewToolImagePersister(
	fileService interfaces.FileService,
	catalog interfaces.ResourceCatalog,
) ToolImagePersister {
	return ToolImagePersister{fileService: fileService, catalog: catalog}
}

// PersistGeneratedImages saves raw image bytes from tool results as message
// artifacts. It returns artifacts in input order; per-image failures are
// logged and skipped so one bad image never breaks the turn.
//
// fileName prefixes the storage name (SaveBytes generates a unique suffix),
// mime selects the extension. Callers pass one entry per tool call so the
// file name can name the producing tool.
func (p ToolImagePersister) PersistGeneratedImages(
	ctx context.Context,
	tenantID uint64,
	messageID string,
	images [][]byte,
	fileName string,
	mime string,
) types.MessageArtifacts {
	if p.fileService == nil || len(images) == 0 || messageID == "" {
		return nil
	}
	ext := generatedImageExt(mime)
	var out types.MessageArtifacts
	for i, raw := range images {
		if len(raw) == 0 {
			continue
		}
		if len(raw) > maxToolGeneratedImageBytes {
			logger.Warnf(ctx, "[ToolImages] skip oversize generated image: index=%d size=%d limit=%d",
				i, len(raw), maxToolGeneratedImageBytes)
			continue
		}
		name := fmt.Sprintf("%s-%d%s", fileName, i+1, ext)
		storageName := "tool-image_" + uuid.NewString() + "_" + name
		storagePath, err := p.fileService.SaveBytes(ctx, raw, tenantID, storageName, false)
		if err != nil {
			logger.Warnf(ctx, "[ToolImages] upload generated image failed: message=%s index=%d err=%v",
				messageID, i, err)
			continue
		}
		if p.catalog != nil {
			if handle, ok := types.ParseResourcePath(storagePath); ok {
				if err := p.catalog.Bind(ctx, types.BuildResourcePath(handle),
					types.ResourceOwnerMessage, messageID, types.ResourceRelationArtifact); err != nil {
					logger.Warnf(ctx, "[ToolImages] bind generated image failed: message=%s ref=%s err=%v",
						messageID, storagePath, err)
				}
			}
		}
		hash := sha256.Sum256(raw)
		out = append(out, types.MessageArtifact{
			URL:         storagePath,
			FileName:    name,
			FileType:    ext,
			FileSize:    int64(len(raw)),
			ContentHash: hex.EncodeToString(hash[:]),
			SourcePath:  "tool-result:" + name,
			CreatedAt:   time.Now().UTC(),
		})
	}
	return out
}

// maxToolGeneratedImageBytes caps a single persisted tool image at 50 MiB —
// the same bound as sandbox skill artifacts (defaultMaxArtifactFileBytes),
// since both flow through SaveBytes in one call.
const maxToolGeneratedImageBytes = 50 * 1024 * 1024

func generatedImageExt(mime string) string {
	switch mime {
	case "image/jpeg":
		return ".jpg"
	case "image/gif":
		return ".gif"
	case "image/webp":
		return ".webp"
	default:
		return ".png"
	}
}

// persistToolGeneratedImages collects GeneratedImages from the turn's agent
// steps and persists them as message artifacts. It runs inside handleComplete
// after the sandbox collect so both artifact kinds share one index space.
func (h *AgentStreamHandler) persistToolGeneratedImages(steps types.AgentSteps) types.MessageArtifacts {
	if h == nil || h.assistantMessage == nil {
		return nil
	}
	images := collectToolGeneratedImages(steps)
	if len(images) == 0 {
		return nil
	}
	ctx := context.WithoutCancel(h.ctx)
	return h.toolImagePersister.PersistGeneratedImages(ctx, h.tenantID, h.assistantMessageID, images, "generated-image", "image/png")
}

// toolGeneratedImageMarkdown cites persisted tool images in the answer body.
// Destinations use the catalog handle when available, falling back to the
// file name so the existing rewrite + hydrate pipeline resolves them.
func toolGeneratedImageMarkdown(artifacts types.MessageArtifacts) string {
	var out []string
	for _, a := range artifacts {
		dest := a.FileName
		if handle, ok := types.ParseResourcePath(a.URL); ok {
			dest = types.BuildResourcePath(handle)
		}
		out = append(out, "![generated image]("+dest+")")
	}
	return joinLines(out)
}

func joinLines(lines []string) string {
	out := ""
	for i, l := range lines {
		if i > 0 {
			out += "\n"
		}
		out += l
	}
	return out
}

// collectToolGeneratedImages walks persisted agent steps and returns the raw
// bytes of every successful tool-produced image, oldest call first.
func collectToolGeneratedImages(steps []types.AgentStep) [][]byte {
	var out [][]byte
	for _, step := range steps {
		for _, tc := range step.ToolCalls {
			if tc.Result == nil || !tc.Result.Success || len(tc.Result.GeneratedImages) == 0 {
				continue
			}
			out = append(out, tc.Result.GeneratedImages...)
		}
	}
	return out
}
