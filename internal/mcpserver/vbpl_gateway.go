package mcpserver

import (
	"context"
	"net/http"

	"github.com/Tencent/WeKnora/internal/vbpl"
)

// The vbpl.vn gateway client lives in internal/vbpl (the SuperAdmin legal
// request queue ingests through the same code); these names keep the MCP
// tools and their tests readable.
const (
	vbplDocURL       = vbpl.DocURL
	vbplTimeout      = vbpl.Timeout
	vbplMinHTMLBytes = vbpl.MinHTMLBytes
)

type (
	vbplDetail  = vbpl.Detail
	vbplNamed   = vbpl.Named
	vbplRelated = vbpl.Related
)

func vbplFetchDetail(ctx context.Context, client *http.Client, id string) (*vbplDetail, error) {
	return vbpl.FetchDetail(ctx, client, id)
}

func vbplFileName(d *vbplDetail) string { return vbpl.FileName(d) }

func vbplMetadata(d *vbplDetail) map[string]string { return vbpl.Metadata(d) }
