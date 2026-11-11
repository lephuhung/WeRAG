package agent

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/Tencent/WeKnora/internal/types"
)

func TestApplyLegalPins(t *testing.T) {
	engine := newTestEngine(t, &mockChat{}, withMaxIterations(20))
	msgs := []chat.Message{{Role: "system", Content: "sys"}, {Role: "user", Content: "Nghị định 53 còn hiệu lực không?"}}
	engine.applyLegalPins(context.Background(), msgs)
	assert.Equal(t, "Nghị định 53 còn hiệu lực không?", msgs[1].Content, "no pin")

	ctx := types.WithLegalReferencePins(context.Background(), []types.LegalReferencePin{{
		Reference: "Nghị định 53", DocumentNumber: "53/2022/NĐ-CP", DocType: "Nghị định", Title: "Quy định chi tiết Luật An ninh mạng",
	}})
	engine.applyLegalPins(ctx, msgs)
	assert.Contains(t, msgs[1].Content, "Nghị định 53 còn hiệu lực không?\n\n[Ghi chú của hệ thống")
	assert.Contains(t, msgs[1].Content, "người dùng đã chọn “Nghị định 53” là Nghị định 53/2022/NĐ-CP (Quy định chi tiết Luật An ninh mạng)")
	assert.Contains(t, msgs[1].Content, "nêu rõ số hiệu này trong câu trả lời")

	// an image turn keeps the note in a text part
	img := []chat.Message{{Role: "user", MultiContent: []chat.MessageContentPart{{Type: "image_url"}}}}
	engine.applyLegalPins(ctx, img)
	assert.Len(t, img[0].MultiContent, 2)
	assert.Contains(t, img[0].MultiContent[1].Text, "53/2022/NĐ-CP")
}
