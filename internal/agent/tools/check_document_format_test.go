package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

type fakeUploads struct {
	interfaces.TemporaryDocumentService
	docs    []*types.TemporaryDocument
	content map[string][]byte
	opened  string
	session string
}

func (f *fakeUploads) List(_ context.Context, _ uint64, sessionID string) ([]*types.TemporaryDocument, error) {
	f.session = sessionID
	return f.docs, nil
}

func (f *fakeUploads) OpenFile(_ context.Context, _ uint64, _ string, id string) (io.ReadCloser, string, error) {
	f.opened = id
	b, ok := f.content[id]
	if !ok {
		return nil, "", errors.New("not found")
	}
	return io.NopCloser(bytes.NewReader(b)), id, nil
}

type chatBase interface{ chat.Chat }

type fakeChat struct {
	chatBase
	opts       *chat.ChatOptions // labeling call (thinking off)
	evalOpts   *chat.ChatOptions // evaluation call (thinking on)
	evalSystem string
	reply      string
	evaluation string // answer to the thinking call; "" → error
	err        error
}

func (f *fakeChat) Chat(_ context.Context, msgs []chat.Message, opts *chat.ChatOptions) (*types.ChatResponse, error) {
	if opts.Thinking != nil && *opts.Thinking {
		f.evalOpts, f.evalSystem = opts, msgs[0].Content
		if f.evaluation == "" {
			return nil, errors.New("evaluation down")
		}
		return &types.ChatResponse{Content: f.evaluation}, nil
	}
	f.opts = opts
	if f.err != nil {
		return nil, f.err
	}
	return &types.ChatResponse{Content: f.reply}, nil
}

func (f *fakeChat) GetModelName() string { return "Qwen/Qwen3.6-35B-A3B-FP8" }

func docxFixture(t *testing.T) []byte {
	t.Helper()
	b, err := os.ReadFile("../../docformat/testdata/parity/fx_good_cong_van_arial.docx")
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func uploadsFixture(t *testing.T) *fakeUploads {
	now := time.Now()
	return &fakeUploads{
		docs: []*types.TemporaryDocument{
			{ID: "old", FileName: "cu.docx", FileType: ".docx", CreatedAt: now.Add(-time.Hour)},
			{ID: "new", FileName: "Cong van so 45.docx", FileType: "docx", CreatedAt: now},
			{ID: "pdf", FileName: "scan.pdf", FileType: ".pdf", CreatedAt: now.Add(time.Minute)},
		},
		content: map[string][]byte{"old": docxFixture(t), "new": docxFixture(t)},
	}
}

func runFormatTool(t *testing.T, tool *CheckDocumentFormatTool, args string) *types.ToolResult {
	t.Helper()
	ctx := context.WithValue(context.Background(), types.TenantIDContextKey, uint64(7))
	res, err := tool.Execute(ctx, json.RawMessage(args))
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func TestCheckDocumentFormatPicksNewestDocx(t *testing.T) {
	up := uploadsFixture(t)
	model := &fakeChat{err: errors.New("model down")} // falls back to heuristic
	res := runFormatTool(t, NewCheckDocumentFormatTool(up, model, "sess-1"), `{}`)
	if !res.Success {
		t.Fatalf("result: %+v", res)
	}
	if up.opened != "new" || up.session != "sess-1" {
		t.Fatalf("opened %q in session %q", up.opened, up.session)
	}
	// labeling and evaluation both fail: heuristic labels, and the agent
	// gets the data with the skills to judge itself
	for _, want := range []string{"Cong van so 45.docx", "noi_dung.font", "Arial", "model down",
		"# Dữ liệu thể thức", `<skill name="the-thuc-cong-van">`} {
		if !strings.Contains(res.Output, want) {
			t.Errorf("output lacks %q:\n%s", want, res.Output)
		}
	}
	if model.opts == nil || model.opts.Thinking == nil || *model.opts.Thinking {
		t.Fatal("labeling must call the agent model with thinking off")
	}
	if res.Data["evaluated"] != false {
		t.Fatalf("evaluated = %v", res.Data["evaluated"])
	}
}

func TestCheckDocumentFormatEvaluatesWithThinking(t *testing.T) {
	up := uploadsFixture(t)
	model := &fakeChat{err: errors.New("labels down"),
		evaluation: "## Kết luận\nChưa đạt: thiếu KT. TRƯỞNG PHÒNG."}
	res := runFormatTool(t, NewCheckDocumentFormatTool(up, model, "s"), `{}`)
	if !res.Success || res.Data["evaluated"] != true {
		t.Fatalf("result: %+v", res)
	}
	if !strings.Contains(res.Output, "thiếu KT. TRƯỞNG PHÒNG") || !strings.Contains(res.Output, "không tự chấm lại") {
		t.Fatalf("output: %s", res.Output)
	}
	if strings.Contains(res.Output, "<skill") {
		t.Fatal("an evaluated result must not hand the skills to the agent again")
	}
	if model.evalOpts == nil || model.evalOpts.MaxTokens < 8192 {
		t.Fatalf("evaluation options = %+v", model.evalOpts)
	}
	if !strings.Contains(model.evalSystem, `<skill name="the-thuc-cong-van">`) {
		t.Fatal("evaluation must receive the document-type skill")
	}
	skills, _ := res.Data["skills"].([]string)
	if len(skills) != 2 {
		t.Fatalf("skills = %v", res.Data["skills"])
	}
}

func TestCheckDocumentFormatByName(t *testing.T) {
	up := uploadsFixture(t)
	res := runFormatTool(t, NewCheckDocumentFormatTool(up, nil, "s"), `{"file_name":"CU.docx"}`)
	if !res.Success || up.opened != "old" {
		t.Fatalf("opened %q: %+v", up.opened, res)
	}
	if res.Data["method"] != "heuristic" {
		t.Fatalf("no model → heuristic, got %v", res.Data["method"])
	}
}

func TestCheckDocumentFormatNoMatch(t *testing.T) {
	up := uploadsFixture(t)
	res := runFormatTool(t, NewCheckDocumentFormatTool(up, nil, "s"), `{"file_name":"khac.docx"}`)
	if res.Success || !strings.Contains(res.Error, "cu.docx") {
		t.Fatalf("result: %+v", res)
	}
	up.docs = up.docs[2:] // only a PDF
	res = runFormatTool(t, NewCheckDocumentFormatTool(up, nil, "s"), `{}`)
	if res.Success || !strings.Contains(res.Error, ".docx") || up.opened != "" {
		t.Fatalf("result: %+v opened=%q", res, up.opened)
	}
}

func TestCheckDocumentFormatNeedsTenant(t *testing.T) {
	res, err := NewCheckDocumentFormatTool(uploadsFixture(t), nil, "s").Execute(context.Background(), nil)
	if err != nil || res.Success {
		t.Fatalf("res=%+v err=%v", res, err)
	}
}
