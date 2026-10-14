package im

import (
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
)

func TestFormatQuotedContext(t *testing.T) {
	tests := []struct {
		name  string
		quote *QuotedMessage
		want  string
	}{
		{
			name:  "nil quote",
			quote: nil,
			want:  "",
		},
		{
			name:  "empty content no NonTextType",
			quote: &QuotedMessage{Content: ""},
			want:  "",
		},
		{
			name:  "non-text image quote generates instruction",
			quote: &QuotedMessage{NonTextType: "image"},
			want:  "Người dùng trích dẫn một tin nhắn hình ảnh mà bạn không xem được. Hãy nói thẳng với người dùng rằng hiện bạn chưa xử lý được tin nhắn hình ảnh và đề nghị họ mô tả vấn đề bằng chữ. Đừng đoán nội dung tin nhắn đó.",
		},
		{
			name:  "non-text file quote generates instruction",
			quote: &QuotedMessage{NonTextType: "file"},
			want:  "Người dùng trích dẫn một tin nhắn tệp mà bạn không xem được. Hãy nói thẳng với người dùng rằng hiện bạn chưa xử lý được tin nhắn tệp và đề nghị họ mô tả vấn đề bằng chữ. Đừng đoán nội dung tin nhắn đó.",
		},
		{
			name:  "non-text unknown type uses fallback label",
			quote: &QuotedMessage{NonTextType: "location"},
			want:  "Người dùng trích dẫn một tin nhắn loại này mà bạn không xem được. Hãy nói thẳng với người dùng rằng hiện bạn chưa xử lý được tin nhắn loại này và đề nghị họ mô tả vấn đề bằng chữ. Đừng đoán nội dung tin nhắn đó.",
		},
		{
			name:  "bot message",
			quote: &QuotedMessage{Content: "bot reply text", IsBotMessage: true},
			want:  "Người dùng trích dẫn câu trả lời trước đó của bạn (bot), chỉ dùng làm ngữ cảnh tham khảo:\n<quoted_message>\nbot reply text\n</quoted_message>",
		},
		{
			name:  "user message",
			quote: &QuotedMessage{Content: "user message text", IsBotMessage: false},
			want:  "Người dùng trích dẫn một tin nhắn trước đó, chỉ dùng làm ngữ cảnh tham khảo:\n<quoted_message>\nuser message text\n</quoted_message>",
		},
		{
			name: "truncation at 500 runes",
			quote: &QuotedMessage{
				Content:      string(make([]rune, 600)),
				IsBotMessage: false,
			},
			want: "Người dùng trích dẫn một tin nhắn trước đó, chỉ dùng làm ngữ cảnh tham khảo:\n<quoted_message>\n" + string(make([]rune, 500)) + "...\n</quoted_message>",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatQuotedContext(tt.quote)
			if got != tt.want {
				t.Errorf("formatQuotedContext() length = %d, want length %d", len(got), len(tt.want))
				if len(got) < 200 && len(tt.want) < 200 {
					t.Errorf("got = %q, want %q", got, tt.want)
				}
			}
		})
	}
}

func TestBuildIMQARequest_QuotedContext(t *testing.T) {
	session := &types.Session{ID: "s1"}

	t.Run("nil quote produces empty QuotedContext", func(t *testing.T) {
		req := buildIMQARequest(session, "hello", "a1", "u1", nil, nil, nil)
		if req.QuotedContext != "" {
			t.Errorf("QuotedContext = %q, want empty", req.QuotedContext)
		}
		if req.Query != "hello" {
			t.Errorf("Query = %q, want %q", req.Query, "hello")
		}
	})

	t.Run("bot quote sets QuotedContext with bot label", func(t *testing.T) {
		quote := &QuotedMessage{Content: "bot reply", IsBotMessage: true}
		req := buildIMQARequest(session, "follow up", "a1", "u1", nil, nil, quote)
		if req.Query != "follow up" {
			t.Errorf("Query = %q, want %q", req.Query, "follow up")
		}
		want := "Người dùng trích dẫn câu trả lời trước đó của bạn (bot), chỉ dùng làm ngữ cảnh tham khảo:\n<quoted_message>\nbot reply\n</quoted_message>"
		if req.QuotedContext != want {
			t.Errorf("QuotedContext = %q, want %q", req.QuotedContext, want)
		}
	})

	t.Run("user quote sets QuotedContext with user label", func(t *testing.T) {
		quote := &QuotedMessage{Content: "user msg", IsBotMessage: false}
		req := buildIMQARequest(session, "question", "a1", "u1", nil, nil, quote)
		want := "Người dùng trích dẫn một tin nhắn trước đó, chỉ dùng làm ngữ cảnh tham khảo:\n<quoted_message>\nuser msg\n</quoted_message>"
		if req.QuotedContext != want {
			t.Errorf("QuotedContext = %q, want %q", req.QuotedContext, want)
		}
	})
}
