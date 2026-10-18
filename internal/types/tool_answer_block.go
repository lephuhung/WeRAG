package types

// ToolDataAnswerBlock is the ToolResult.Data key under which a tool leaves
// Markdown that code appends to the final answer (agent.appendToolAnswerBlocks),
// so a result the model must not drop or misread — a calculation, its
// assumptions and formulas — reaches the user as computed. The tool builds
// it from its structured result, never from re-parsed text.
const ToolDataAnswerBlock = "answer_block"

// ToolAnswerBlockMarker opens the appended block. It is stable so the block
// (and the footer after it) can be cut when the answer is replayed as
// history, like the domain-pack footer.
const ToolAnswerBlockMarker = "\n\n---\n**Kết quả tính (do hệ thống tính bằng công cụ)**\n"
