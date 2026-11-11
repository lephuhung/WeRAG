package agent

import (
	"context"

	agenttools "github.com/Tencent/WeKnora/internal/agent/tools"
	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/Tencent/WeKnora/internal/types"
)

// A request to check the legal basis of the open document ("rà soát căn
// cứ pháp lý", "các văn bản viện dẫn còn hiệu lực không") reads to the
// knowledge-lookup classifier as a legal question: the turn would start
// with search_knowledge under the grounding gate and the model answer
// from search hits, citation by citation from memory. When the request is
// one (agenttools.IsLegalBasisCheckRequest) and the tool is offered, the
// turn starts with check_document_legal_basis instead; its report, read
// from the legal catalogue, is the turn's evidence (grounding_gate.go).

// legalBasisFirstTool is check_document_legal_basis when the turn's request
// is a legal basis check and the tool is offered, "" otherwise.
func legalBasisFirstTool(ctx context.Context, tools []chat.Tool) string {
	if !hasChatTool(tools, agenttools.ToolCheckDocumentLegalBasis) ||
		!agenttools.IsLegalBasisCheckRequest(types.UserQueryFromContext(ctx)) {
		return ""
	}
	return agenttools.ToolCheckDocumentLegalBasis
}
