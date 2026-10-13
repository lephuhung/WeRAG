package tools

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/Tencent/WeKnora/internal/docformat/docxedit"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/google/uuid"
)

// Illustrations for the document assistant: a diagram (draw_diagram, drawn
// from Mermaid source by the browser, so Vietnamese labels come out exactly
// as written) or a picture (generate_illustration, painted by the image
// model behind an MCP service, Qwen-Image). Both only PROPOSE: the result
// carries Data.illustration and the chat card inserts the picture into the
// editor (op insertImage) when the user presses "Chèn vào văn bản".

// Data.illustration kinds.
const (
	IllustrationDiagram = "diagram"
	IllustrationImage   = "image"
)

// OpInsertImage is the editor op the chat card builds once it has the
// picture's bytes (a PNG data URL); the server never sends it.
const OpInsertImage = "insertImage"

const (
	maxMermaidRunes      = 6000
	maxIllustrationTitle = 120
	maxIllustrationAlt   = 300
	// default printed width: the text column of an A4 page under NĐ30
	// margins (30 mm left, 15–20 mm right) is about 160 mm
	defaultIllustrationWidthMm = 150
)

// illustrationAnchorSchema is the "where to insert" part both tools take.
const illustrationAnchorSchema = `"after_match": {"type": "string", "description": "Preferred: the opening words of the paragraph the picture goes after, copied from the document (e.g. \"Bước 5: Ủy ban nhân dân\")"},
    "after": {"type": "integer", "minimum": 0, "description": "Index (from read_document_outline) of the paragraph the picture goes after; easy to miscount, use after_match when you can. Ignored when after_match is given"},
    "caption": {"type": "string", "description": "Optional caption printed under the picture, e.g. \"Hình 1. Quy trình tiếp nhận hồ sơ\""}`

const illustrationAnchorDoc = `Where it goes: after_match (or after) names the paragraph the picture is inserted after; without them it goes after the last paragraph of the highlighted passage. Prefer after_match with the paragraph's own opening words — a paragraph index is easily off by one. The result names the paragraph it chose: if that is not the one the user meant, call again with after_match. With neither (no selection, no document) the user can still download the picture.`

// illustrationProposalNote ends the Output of both tools.
const illustrationProposalNote = "Văn bản CHƯA thay đổi: hình hiện dưới câu trả lời với nút “Chèn vào văn bản”. " +
	"Giới thiệu ngắn hình vừa tạo và mời người dùng xem, chèn hoặc yêu cầu chỉnh lại — không nói là đã chèn, " +
	"không chép lại mã sơ đồ vào câu trả lời."

// illustrationTarget is where the picture would be inserted.
type illustrationTarget struct {
	ws     *types.DocumentWorkspace
	anchor *OpAnchor
	after  int // paragraph index, -1 when unknown
	note   string
}

// resolveIllustrationTarget finds the document and the paragraph the picture
// would follow. Failing to find either is not an error: the picture can
// still be shown and downloaded, the reason goes into note.
func resolveIllustrationTarget(ctx context.Context, src DocumentWorkspaceSource, sessionID, ref string,
	after *int, afterMatch string,
) illustrationTarget {
	out := illustrationTarget{after: -1}
	if src == nil {
		return out
	}
	target, err := resolveDocument(ctx, src, sessionID, ref, true)
	if err != nil {
		if strings.TrimSpace(ref) != "" || after != nil || strings.TrimSpace(afterMatch) != "" {
			out.note = err.Error()
		}
		return out
	}
	content, ws, err := readListedDocument(ctx, src, sessionID, target)
	if err != nil {
		out.note = err.Error()
		return out
	}
	doc, err := docxedit.Open(content)
	if err != nil {
		out.note = "không đọc được tài liệu: " + err.Error()
		return out
	}
	out.ws = ws
	paras := doc.Paragraphs()
	idx, msg := -1, ""
	switch {
	case strings.TrimSpace(afterMatch) != "":
		// the paragraph's own words beat an index the model may have miscounted
		idx, msg = resolveInsertAnchor(doc, paras, insertSpec{AfterMatch: afterMatch})
	case after != nil:
		idx, msg = resolveInsertAnchor(doc, paras, insertSpec{After: after})
	default:
		sel := types.DocumentSelectionFromContext(ctx)
		if !selectionIn(sel, ws) {
			msg = "chưa biết chèn ở đâu: người dùng chưa bôi đen đoạn nào và lệnh gọi không có after / after_match"
			break
		}
		idx = lastSelectedParagraph(paras, sel)
		if idx < 0 {
			msg = "không tìm thấy đoạn đã bôi đen trong tài liệu"
		}
	}
	if msg != "" || idx < 0 {
		out.note = msg
		return out
	}
	out.after = idx
	out.anchor = newVirtualDoc(paras).anchor(idx)
	return out
}

// lastSelectedParagraph is the index of the last non-empty paragraph the
// selection covers, or -1. A paragraph inside the selection (or holding
// it) wins over one that merely shares words with it.
func lastSelectedParagraph(paras []docxedit.Paragraph, sel *types.DocumentSelection) int {
	contained, overlapping := -1, -1
	for _, probe := range []string{sel.Text, sel.ParagraphHint} {
		s := foldForOverlap(probe)
		if s == "" {
			continue
		}
		for i, p := range paras {
			t := foldForOverlap(p.Text)
			if t == "" {
				continue
			}
			switch {
			// a tiny paragraph ("a)", "./.") elsewhere must not count as
			// selected just because the passage contains its text
			case strings.Contains(t, s) || (utf8.RuneCountInString(t) >= 12 && strings.Contains(s, t)):
				contained = i
			case selectionOverlaps(probe, p.Text):
				overlapping = i
			}
		}
		if contained >= 0 {
			return contained
		}
	}
	return overlapping
}

// illustrationData is the Data of both tools; the chat card reads it.
func illustrationData(kind string, t illustrationTarget, fields map[string]interface{}) map[string]interface{} {
	ill := map[string]interface{}{
		"kind":     kind,
		"width_mm": defaultIllustrationWidthMm,
	}
	for k, v := range fields {
		ill[k] = v
	}
	data := map[string]interface{}{
		"illustration": ill,
		"ops_batch_id": uuid.NewString(),
	}
	if t.ws != nil {
		data["document_id"] = t.ws.ID
		data["document"] = DocumentLabel(t.ws)
		data["file_name"] = t.ws.FileName
	}
	if t.anchor != nil {
		ill["anchor"] = t.anchor
		ill["after"] = t.after
	}
	return data
}

func writeIllustrationPlace(out *strings.Builder, t illustrationTarget) {
	switch {
	case t.anchor != nil:
		fmt.Fprintf(out, "Vị trí chèn đề xuất: sau đoạn [%d] “%s” của %s.\n", t.after, clipRunes(t.anchor.Text, 80), t.ws.FileName)
	case t.note != "":
		fmt.Fprintf(out, "Chưa xác định được vị trí chèn (%s); người dùng vẫn tải được hình. Muốn chèn, hãy hỏi họ chèn sau đoạn nào hoặc nhắc họ bôi đen vị trí.\n", t.note)
	}
}

func cleanCaption(s string) string {
	return clipRunes(strings.Join(strings.Fields(s), " "), maxIllustrationTitle)
}

/* ---------- draw_diagram ---------- */

var drawDiagramTool = BaseTool{
	name: ToolDrawDiagram,
	description: `Draw a diagram (sơ đồ) for the document: a process / workflow (quy trình), an organisation chart (sơ đồ tổ chức), a hierarchy, a timeline, a mind map, the steps of a procedure, the relations between agencies. You write the diagram in Mermaid; the user's browser draws it, so every Vietnamese label appears exactly as you wrote it. Nothing is inserted: the diagram is shown under your answer with a "Chèn vào văn bản" button.

## When to Use

The user asks to draw, sketch or visualise something as a diagram ("vẽ sơ đồ", "sơ đồ hóa", "lưu đồ", "sơ đồ tổ chức", "quy trình dạng sơ đồ", "minh họa bằng sơ đồ"), usually for a highlighted passage. Base the diagram ONLY on the passage / document content: do not invent steps, units or relations that the text does not state. For a photo-like or artistic picture use generate_illustration instead.

## When it cannot be drawn — tell the user, do not force it

Before calling, check that the content really has something to draw: steps in an order, units and who they report to, dates, parts of a whole, actors and what they pass to each other. When it does not (a single statement, a legal citation, a greeting, a paragraph of general opinion, too little detail), do NOT call the tool and do NOT make a diagram up: tell the user why this passage cannot be drawn as a diagram and ask what they want shown (which steps, which units), or suggest highlighting another passage. A diagram with fewer than two elements is refused for the same reason.

## Mermaid rules

- Start with one of: flowchart TD | flowchart LR | sequenceDiagram | stateDiagram-v2 | timeline | mindmap | gantt | pie.
- Organisation charts and procedures: flowchart TD; a long linear process: flowchart LR.
- Put EVERY label in double quotes: A["Tiếp nhận hồ sơ"] --> B{"Hồ sơ hợp lệ?"}; B -->|"Có"| C["Thẩm định"]. Never use " inside a label (use ' instead), no HTML, no Markdown, no emoji.
- Short labels (at most ~8 words), node ids in ASCII (A, B, n1 …), at most ~25 nodes. No %%{init}%% directives, no click / href / call statements, no styles that hide text.

## Input

- mermaid: the diagram source.
- title: a short Vietnamese title for the card.
- ` + illustrationAnchorDoc + `
- caption: optional caption under the picture.`,
	schema: json.RawMessage(`{
  "type": "object",
  "properties": {
    ` + documentParamSchema + `,
    "mermaid": {"type": "string", "description": "Mermaid source of the diagram (labels in Vietnamese, in double quotes)"},
    "title": {"type": "string", "description": "Short Vietnamese title"},
    ` + illustrationAnchorSchema + `
  },
  "required": ["mermaid", "title"]
}`),
}

type drawDiagramInput struct {
	Document   string `json:"document"`
	Mermaid    string `json:"mermaid"`
	Title      string `json:"title"`
	After      *int   `json:"after"`
	AfterMatch string `json:"after_match"`
	Caption    string `json:"caption"`
}

// DrawDiagramTool checks a Mermaid diagram and proposes it for insertion.
type DrawDiagramTool struct {
	BaseTool
	workspace DocumentWorkspaceSource
	sessionID string
}

// NewDrawDiagramTool builds the tool for one session; workspace may be nil
// (the diagram is then only shown and downloadable).
func NewDrawDiagramTool(workspace DocumentWorkspaceSource, sessionID string) *DrawDiagramTool {
	return &DrawDiagramTool{BaseTool: drawDiagramTool, workspace: workspace, sessionID: sessionID}
}

// mermaidDiagramTypes are the diagram headers draw_diagram accepts.
var mermaidDiagramTypes = []string{
	"flowchart", "graph", "sequenceDiagram", "stateDiagram-v2", "stateDiagram",
	"timeline", "mindmap", "gantt", "pie",
}

var (
	mermaidDirective = regexp.MustCompile(`%%\{`)
	// click / href / call bind script or links to nodes
	mermaidInteraction = regexp.MustCompile(`(?m)^\s*(click|href|call)\b`)
)

// checkMermaid normalises the source and returns why it cannot be drawn.
func checkMermaid(src string) (string, string) {
	src = strings.TrimSpace(strings.ReplaceAll(src, "\r\n", "\n"))
	// a model that JSON-escaped twice leaves \" in labels (-->|\"Có\"|):
	// never valid Mermaid, and the browser would refuse the whole diagram
	src = strings.ReplaceAll(src, `\"`, `"`)
	// a model that wrote the whole source on one line with a literal
	// backslash-n between the lines: counted as one line, the diagram was
	// refused as "fewer than two elements" and the model retried 30 times
	if !strings.Contains(src, "\n") && strings.Contains(src, `\n`) {
		src = strings.ReplaceAll(src, `\n`, "\n")
	}
	// a fenced block from a model that wrote Markdown
	if strings.HasPrefix(src, "```") {
		src = strings.TrimPrefix(src, "```mermaid")
		src = strings.TrimPrefix(src, "```")
		src = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(src), "```"))
	}
	if src == "" {
		return "", "mermaid trống"
	}
	if utf8.RuneCountInString(src) > maxMermaidRunes {
		return "", fmt.Sprintf("sơ đồ quá dài (tối đa %d ký tự): hãy rút gọn hoặc tách thành nhiều sơ đồ", maxMermaidRunes)
	}
	if mermaidDirective.MatchString(src) {
		return "", "không dùng chỉ thị %%{init}%%"
	}
	if mermaidInteraction.MatchString(src) {
		return "", "không dùng click / href / call trong sơ đồ"
	}
	if strings.Contains(strings.ToLower(src), "<script") {
		return "", "nhãn không được chứa HTML"
	}
	first := ""
	for _, line := range strings.Split(src, "\n") {
		if l := strings.TrimSpace(line); l != "" && !strings.HasPrefix(l, "%%") {
			first = l
			break
		}
	}
	head := strings.Fields(first)
	if len(head) == 0 {
		return "", "mermaid trống"
	}
	for _, t := range mermaidDiagramTypes {
		if head[0] == t {
			if mermaidElements(src) < 2 {
				return "", errDiagramTooThin
			}
			return src, ""
		}
	}
	return "", fmt.Sprintf("dòng đầu phải là loại sơ đồ (%s), không phải %q",
		strings.Join(mermaidDiagramTypes, ", "), clipRunes(first, 40))
}

// errDiagramTooThin: the content gives nothing to draw.
const errDiagramTooThin = "dữ liệu không đủ để vẽ sơ đồ (ít hơn hai thành phần). " +
	"Đừng tự bịa thêm: báo người dùng đoạn này không có cấu trúc để vẽ thành sơ đồ và hỏi họ muốn thể hiện nội dung gì (các bước, các đơn vị…), hoặc nhờ bôi đen đoạn khác"

// mermaidElements roughly counts what the diagram shows: one per body line
// (not the header, comments or blank lines), two for a line joining two
// nodes with an arrow.
func mermaidElements(src string) int {
	n, header := 0, true
	for _, line := range strings.Split(src, "\n") {
		l := strings.TrimSpace(line)
		if l == "" || strings.HasPrefix(l, "%%") {
			continue
		}
		if header {
			header = false
			continue
		}
		n++
		if strings.Contains(l, "--") || strings.Contains(l, "->") || strings.Contains(l, "==") {
			n++
		}
	}
	return n
}

func (t *DrawDiagramTool) Execute(ctx context.Context, args json.RawMessage) (*types.ToolResult, error) {
	var in drawDiagramInput
	if err := json.Unmarshal(args, &in); err != nil {
		return &types.ToolResult{Success: false, Error: "invalid arguments: " + err.Error()}, nil
	}
	src, msg := checkMermaid(in.Mermaid)
	if msg == errDiagramTooThin {
		return &types.ToolResult{Success: false, Error: "Không vẽ được sơ đồ: " + msg + "."}, nil
	}
	if msg != "" {
		return &types.ToolResult{Success: false, Error: "Không vẽ được sơ đồ: " + msg + ". Hãy sửa mermaid rồi gọi lại."}, nil
	}
	title := cleanCaption(in.Title)
	if title == "" {
		title = "Sơ đồ"
	}
	target := resolveIllustrationTarget(ctx, t.workspace, t.sessionID, in.Document, in.After, in.AfterMatch)

	var out strings.Builder
	fmt.Fprintf(&out, "Đã dựng sơ đồ “%s” (%d dòng mermaid); trình duyệt sẽ vẽ nó dưới câu trả lời.\n", title, strings.Count(src, "\n")+1)
	writeIllustrationPlace(&out, target)
	out.WriteString("Nếu thẻ sơ đồ báo lỗi cú pháp, người dùng sẽ nhờ vẽ lại: khi đó sửa mermaid theo quy tắc của tool.\n")
	out.WriteString("\n" + illustrationProposalNote + "\n")
	data := illustrationData(IllustrationDiagram, target, map[string]interface{}{
		"title":   title,
		"caption": cleanCaption(in.Caption),
		"mermaid": src,
	})
	return &types.ToolResult{Success: true, Output: out.String(), Data: data}, nil
}

/* ---------- generate_illustration ---------- */

// errVietnamesePrompt refuses a prompt with Vietnamese letters: the image
// model draws Vietnamese text wrongly.
const errVietnamesePrompt = "Không tạo hình: prompt có chữ tiếng Việt. Mô hình tạo ảnh KHÔNG viết được tiếng Việt " +
	"(chữ và dấu bị sai, méo). Viết lại prompt bằng tiếng Anh và yêu cầu hình không có chữ. " +
	"Nếu người dùng cần chữ tiếng Việt trong hình, đừng gọi lại: giải thích giới hạn này và đề xuất " +
	"hình không chữ kèm chú thích (caption) bên dưới, hoặc sơ đồ có nhãn tiếng Việt (draw_diagram)."

// hasVietnameseLetters reports letters only Vietnamese uses: ă â đ ê ô ơ ư
// and the tone-marked vowels of the Latin Extended Additional block.
func hasVietnameseLetters(s string) bool {
	for _, r := range s {
		switch {
		case r >= 0x1EA0 && r <= 0x1EF9:
			return true
		case strings.ContainsRune("ăâđêôơưĂÂĐÊÔƠƯ", r):
			return true
		}
	}
	return false
}

// unreachableMarkers name a lost or silent image service rather than a
// request it refused.
var unreachableMarkers = []string{
	"connection refused", "connection reset", "no such host", "dial tcp", "eof", "timeout", "timed out",
	"deadline exceeded", "unavailable", "502", "503", "504", "broken pipe", "not connected",
	"failed to connect", "connect", "cuda", "out of memory",
}

// serviceFaultMarkers name an error the image service hit on its own side.
// An MCP server wraps whatever its tool raised ("Error executing tool X"),
// and the arguments were already checked here, so such an error is the
// service failing (a crashed model server, a lost GPU), not the request.
var serviceFaultMarkers = []string{"error executing tool", "internal error", "internal server error", "api error"}

func serviceFault(low string) bool {
	for _, m := range serviceFaultMarkers {
		if strings.Contains(low, m) {
			return true
		}
	}
	return false
}

// imageFailure is the error generate_illustration returns: the model must
// tell the user, never retry in the same turn.
func imageFailure(service, reason string, cancelled bool) string {
	low := strings.ToLower(reason)
	unreachable := cancelled
	for _, m := range unreachableMarkers {
		if strings.Contains(low, m) {
			unreachable = true
			break
		}
	}
	if !unreachable && serviceFault(low) {
		return fmt.Sprintf("Không tạo được hình: dịch vụ tạo ảnh %s gặp lỗi nội bộ khi vẽ (%s) — lỗi ở phía dịch vụ, "+
			"KHÔNG phải do nội dung yêu cầu. KHÔNG thử lại trong lượt này và đừng đề nghị người dùng đổi mô tả. Báo người dùng "+
			"dịch vụ tạo ảnh đang gặp sự cố và hỏi họ muốn thử lại sau hay vẽ sơ đồ (draw_diagram) thay thế.", service, reason)
	}
	if unreachable {
		return fmt.Sprintf("Không tạo được hình: dịch vụ tạo ảnh %s đang mất kết nối hoặc không phản hồi (%s). "+
			"KHÔNG thử lại trong lượt này. Báo người dùng dịch vụ tạo ảnh tạm thời không dùng được và hỏi họ "+
			"muốn thử lại sau hay vẽ sơ đồ (draw_diagram) thay thế.", service, reason)
	}
	return fmt.Sprintf("Không tạo được hình (%s): %s. KHÔNG thử lại trong lượt này. Báo người dùng lý do và hỏi họ "+
		"muốn điều chỉnh yêu cầu thế nào (nội dung, bố cục, phong cách) hoặc dùng sơ đồ thay thế.", service, reason)
}

var generateIllustrationTool = BaseTool{
	name: ToolGenerateIllustration,
	description: `Paint an illustration (hình minh họa) with the image model: a cover picture, a scene, an icon-like drawing, a concept picture for a report or a propaganda / awareness document. Nothing is inserted: the picture is shown under your answer with a "Chèn vào văn bản" button. It takes up to a few minutes.

## When to Use

The user asks for a picture, image or illustration ("tạo hình minh họa", "vẽ hình", "ảnh minh họa", "hình ảnh cho trang bìa"). For a diagram with boxes, arrows and labels (process, organisation chart, steps) use draw_diagram.

## The image model cannot write Vietnamese

The picture can carry NO Vietnamese text: the model draws Vietnamese letters and diacritics wrongly (garbled or missing dấu). So:
- The prompt is in English and asks for no text at all; a prompt containing Vietnamese is refused.
- When the user wants words in the picture (a title, a slogan, labels, a banner text), say plainly that the image model cannot write Vietnamese, then offer: the picture without text plus the words as a caption under it (caption), or a diagram with exact Vietnamese labels (draw_diagram). Do not call the tool until the user accepts one.

## When it fails — tell the user

If the result says the image service is unreachable (mất kết nối, không phản hồi), do not retry in this turn: tell the user the image service is not available right now, and ask whether to try again later or to draw a diagram instead. If the request itself cannot be pictured (too abstract, or it needs real faces, emblems or text), say so and ask the user how to adjust it.

## Prompt

Write prompt in ENGLISH, 1–4 sentences: subject, setting, composition, style (e.g. "clean flat vector illustration, soft colours, white background"), mood. Ask for NO text, letters, numbers or logos in the picture. Keep it suitable for an official Vietnamese government document: neutral, respectful, no real persons' faces, no national emblem, flag or party symbols unless the user explicitly asks, nothing violent or political.

## Input

- prompt: the English image prompt (no Vietnamese, no text in the picture).
- title: a short Vietnamese title for the card.
- aspect: landscape (default) | square | portrait | wide.
- ` + illustrationAnchorDoc + `
- caption: optional caption under the picture.`,
	schema: json.RawMessage(`{
  "type": "object",
  "properties": {
    ` + documentParamSchema + `,
    "prompt": {"type": "string", "description": "English prompt for the image model, asking for no text in the picture"},
    "title": {"type": "string", "description": "Short Vietnamese title"},
    "aspect": {"type": "string", "enum": ["landscape", "square", "portrait", "wide"]},
    ` + illustrationAnchorSchema + `
  },
  "required": ["prompt", "title"]
}`),
}

// illustrationSizes are Qwen-Image's native resolutions per aspect.
var illustrationSizes = map[string][2]int{
	"landscape": {1472, 1104}, // 4:3
	"square":    {1328, 1328},
	"portrait":  {1104, 1472},
	"wide":      {1664, 928}, // 16:9
}

// illustrationSteps trades quality for time on the shared GPU.
const illustrationSteps = 24

type generateIllustrationInput struct {
	Document   string `json:"document"`
	Prompt     string `json:"prompt"`
	Title      string `json:"title"`
	Aspect     string `json:"aspect"`
	After      *int   `json:"after"`
	AfterMatch string `json:"after_match"`
	Caption    string `json:"caption"`
}

// ImageGenerator paints a picture: the MCP tool (generate_image of the
// image service) whose result carries GeneratedImages.
type ImageGenerator interface {
	Execute(ctx context.Context, args json.RawMessage) (*types.ToolResult, error)
}

// GenerateIllustrationTool asks the image model for a picture and proposes
// it for insertion.
type GenerateIllustrationTool struct {
	BaseTool
	images    ImageGenerator
	service   string // the image service's name, for messages
	workspace DocumentWorkspaceSource
	sessionID string
}

// NewGenerateIllustrationTool builds the tool for one session.
func NewGenerateIllustrationTool(images ImageGenerator, serviceName string,
	workspace DocumentWorkspaceSource, sessionID string,
) *GenerateIllustrationTool {
	return &GenerateIllustrationTool{BaseTool: generateIllustrationTool, images: images, service: serviceName,
		workspace: workspace, sessionID: sessionID}
}

func (t *GenerateIllustrationTool) Execute(ctx context.Context, args json.RawMessage) (*types.ToolResult, error) {
	var in generateIllustrationInput
	if err := json.Unmarshal(args, &in); err != nil {
		return &types.ToolResult{Success: false, Error: "invalid arguments: " + err.Error()}, nil
	}
	prompt := strings.TrimSpace(in.Prompt)
	if prompt == "" {
		return &types.ToolResult{Success: false, Error: "prompt trống"}, nil
	}
	prompt = clipRunes(prompt, 1500)
	if hasVietnameseLetters(prompt) {
		return &types.ToolResult{Success: false, Error: errVietnamesePrompt}, nil
	}
	aspect := in.Aspect
	if aspect == "" {
		aspect = "landscape"
	}
	size, ok := illustrationSizes[aspect]
	if !ok {
		return &types.ToolResult{Success: false, Error: "aspect phải là landscape, square, portrait hoặc wide"}, nil
	}
	if t.images == nil {
		return &types.ToolResult{Success: false, Error: "Chưa cấu hình dịch vụ tạo ảnh."}, nil
	}
	title := cleanCaption(in.Title)
	if title == "" {
		title = "Hình minh họa"
	}
	// where it goes is known before the (slow) image call
	target := resolveIllustrationTarget(ctx, t.workspace, t.sessionID, in.Document, in.After, in.AfterMatch)

	callArgs, _ := json.Marshal(map[string]interface{}{
		"prompt": prompt, "width": size[0], "height": size[1], "steps": illustrationSteps,
	})
	res, err := t.images.Execute(ctx, callArgs)
	if err != nil || res == nil || !res.Success || len(res.GeneratedImages) == 0 {
		reason := "dịch vụ tạo ảnh không trả về hình"
		switch {
		case err != nil:
			reason = err.Error()
		case res != nil && strings.TrimSpace(res.Error) != "":
			reason = strings.TrimSpace(res.Error)
		}
		return &types.ToolResult{Success: false, Error: imageFailure(t.service, clipRunes(reason, 300), ctx.Err() != nil)}, nil
	}
	img := res.GeneratedImages[0]
	sum := sha256.Sum256(img)

	var out strings.Builder
	fmt.Fprintf(&out, "Đã tạo hình “%s” (%dx%d) bằng %s.\n", title, size[0], size[1], t.service)
	writeIllustrationPlace(&out, target)
	out.WriteString("\n" + illustrationProposalNote + "\n")
	data := illustrationData(IllustrationImage, target, map[string]interface{}{
		"title":        title,
		"caption":      cleanCaption(in.Caption),
		"prompt":       clipRunes(prompt, maxIllustrationAlt),
		"width":        size[0],
		"height":       size[1],
		"image_sha256": hex.EncodeToString(sum[:]),
	})
	return &types.ToolResult{
		Success: true,
		Output:  out.String(),
		Data:    data,
		// persisted as a message artifact when the turn ends (and shown in
		// the answer); the card finds it by image_sha256
		// the answer model does not need the pixels: no Images
		GeneratedImages: [][]byte{img},
	}, nil
}
