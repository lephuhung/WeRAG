package service

import (
	"context"
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/event"
	"github.com/Tencent/WeKnora/internal/types"
)

func TestUnreadableAttachmentsMessage(t *testing.T) {
	photoErr := "parsing failed: " + ImageNoTextMessage
	// The 2026-10-16 case: one photo, failed, nothing else → the policy
	// sentence alone; the file name is on the chip already.
	req := &types.QARequest{Query: "Cho tôi thông tin ảnh này", Attachments: types.MessageAttachments{
		{ID: "a", FileName: "494539404_n.jpg", FileType: ".jpg", ParseError: photoErr},
	}}
	msg, ok := unreadableAttachmentsMessage(req)
	if !ok || !strings.Contains(msg, "chỉ nhận ảnh chụp văn bản") || strings.Contains(msg, "494539404") || strings.Contains(msg, "parsing failed") {
		t.Fatalf("ok=%v msg=%q", ok, msg)
	}
	// Two failed files are listed by name.
	two := &types.QARequest{Attachments: types.MessageAttachments{
		{FileName: "a.jpg", ParseError: photoErr}, {FileName: "b.pdf", ParseError: "parsing failed: Không đọc được chữ trong tệp PDF."},
	}}
	if msg, ok := unreadableAttachmentsMessage(two); !ok || !strings.Contains(msg, "- a.jpg: ") || !strings.Contains(msg, "- b.pdf: ") {
		t.Fatalf("ok=%v msg=%q", ok, msg)
	}
	// One readable file among the failed ones: the model runs with what it has.
	req.Attachments = append(req.Attachments, types.MessageAttachment{ID: "b", FileName: "x.docx", Content: "Điều 1. ..."})
	if _, ok := unreadableAttachmentsMessage(req); ok {
		t.Fatal("a readable sibling must let the turn run")
	}
	// A failed parse but the image itself forwarded to a vision model: not blocked.
	req.Attachments = req.Attachments[:1]
	req.ImageURLs = []string{"resource://img"}
	if _, ok := unreadableAttachmentsMessage(req); ok {
		t.Fatal("a forwarded image must let the turn run")
	}
	// No attachments at all: never.
	if _, ok := unreadableAttachmentsMessage(&types.QARequest{Query: "hi"}); ok {
		t.Fatal("no attachments, no gate")
	}
	// English-only internal errors are not shown verbatim.
	if got := attachmentFailureCause("failed to parse document: rpc error: code = Unavailable"); got != "tệp không đọc được." {
		t.Fatalf("cause = %q", got)
	}
}

func TestUnreadableAttachmentsAnswerEndsAgentTurn(t *testing.T) {
	bus := event.NewEventBus()
	var final string
	completed := false
	bus.On(event.EventAgentFinalAnswer, func(_ context.Context, e event.Event) error {
		if d, ok := e.Data.(event.AgentFinalAnswerData); ok && d.Content != "" {
			final = d.Content
		}
		return nil
	})
	bus.On(event.EventAgentComplete, func(_ context.Context, e event.Event) error { completed = true; return nil })
	req := &types.QARequest{Query: "Ảnh này nói về gì", Session: &types.Session{ID: "s1"}, AssistantMessageID: "m1",
		Attachments: types.MessageAttachments{{ID: "a", FileName: "anh.jpg", ParseError: "parsing failed: " + ImageNoTextMessage}}}
	svc := &sessionService{}
	answered, err := svc.unreadableAttachmentsAnswer(context.Background(), req, bus, true)
	if err != nil || !answered || !completed || !strings.Contains(final, "chỉ nhận ảnh chụp văn bản") {
		t.Fatalf("answered=%v err=%v completed=%v final=%q", answered, err, completed, final)
	}
}
