// legal_graph.go is the legal-document path of the per-chunk graph
// extraction task. It ports AIRAG's LegalKGService behavior (prompts +
// deterministic post-processing) onto WeRAG's chunk task model:
//
//   - The prompt is AIRAG's tuned Vietnamese legal extraction prompt with
//     the closed entity/relation vocabulary, composite-person-key rules and
//     generic-entity bans. document_meta (số hiệu, cơ quan ban hành, ngày
//     ban hành) is injected so the model can disambiguate short names.
//   - PostProcessLegalGraph is the deterministic resolution layer: it
//     canonicalizes names (số hiệu → Document merge key), folds
//     self-references onto the document root, drops generic/junk entities,
//     reroutes relation endpoints through the canonical map, drops
//     self-loops/junk endpoints, and synthesizes the structural edges
//     (PART_OF, BAN_HANH_BOI, CAN_CU) AIRAG injects per article.
//
// No doc-level LLM entity-resolution pass is used — per the agreed scope,
// canonicalization is deterministic only.
package chatpipeline

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/Tencent/WeKnora/internal/common"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/vietnamese_legal"
)

// LegalKGSystemPrompt is a port of AIRAG LEGAL_KG_SYSTEM_PROMPT — the
// closed-vocabulary legal extraction prompt for general legal documents.
const LegalKGSystemPrompt = `Bạn là chuyên gia phân tích văn bản hành chính/pháp luật Việt Nam.
Nhiệm vụ của bạn là trích xuất các thực thể (entities) và mối quan hệ (relations) từ một Điều/Khoản của văn bản được cung cấp.

## Các loại thực thể được phép (entity types):
- Article: Điều, Khoản, Điểm của văn bản hiện tại. Ví dụ: "Điều 5", "Khoản 2 Điều 3"
- Document: Văn bản pháp luật được viện dẫn. Ví dụ: "Nghị định 123/2024/NĐ-CP".
  • Dùng tên ĐỊNH DANH NGẮN GỌN NHẤT. Nếu có số hiệu → ưu tiên kèm số hiệu.
  • TUYỆT ĐỐI KHÔNG thêm hậu tố ngày/tháng/năm ban hành vào tên.
    ĐÚNG: "Luật An ninh mạng"  —  SAI: "Luật An ninh mạng ngày 12 tháng 6 năm 2018".
- Organization: Cơ quan, tổ chức CỤ THỂ, có tên riêng định danh được. PHẢI bổ sung tên đầy đủ dựa vào (issuing_agency). Ví dụ: "UBND Tỉnh Nghệ An" (không dùng "UBND tỉnh").
  • KHÔNG trích xuất tổ chức CHUNG CHUNG / không định danh được. BỎ QUA hoàn toàn các trường hợp:
    - tên trống nghĩa: "Bộ", "Bộ trưởng", "Cơ quan", "Đơn vị", "Doanh nghiệp", "Tổ chức".
    - loại cơ quan: "Cơ quan ngang Bộ", "Cơ quan thuộc Chính phủ", "Cơ quan nhà nước", "Tổ chức chính trị".
    - nhóm/liệt kê: "các cơ quan, tổ chức, cá nhân có liên quan", "Bộ, ngành có liên quan", "các cơ quan Đảng, Nhà nước ở trung ương", "tổ chức, cá nhân có liên quan".
    - placeholder biểu mẫu: bất cứ tên nào chứa "(tên đơn vị...)", "(tên cơ quan...)", "(chủ quản...)".
    - tên biểu mẫu/văn bản con: "Mẫu số 02", "Tờ trình", "Đơn đề nghị".
- Person: Cá nhân. PHẢI dùng Composite Key theo quy tắc ưu tiên (xem bên dưới). Các cá nhân chung chung như "Người có trách nhiệm", "Người liên quan" không được coi là Person
- Task: Nhiệm vụ/công việc cụ thể được giao. HÃY trích xuất MỖI nhiệm vụ chính trong điều khoản, viết dưới dạng CỤM ĐỘNG TỪ NGẮN GỌN (3–6 từ), chuẩn hóa. ĐỪNG bỏ sót nhiệm vụ.
  • Nếu văn bản diễn đạt nhiệm vụ bằng câu dài, PHẢI RÚT GỌN về cụm động từ cốt lõi — KHÔNG sao chép nguyên câu/mệnh đề làm tên Task.
  • Mỗi Task nên gắn với chủ thể thực hiện qua quan hệ CHU_TRI / CHIU_TRACH_NHIEM / PHOI_HOP (Task → Organization/Person).
    ĐÚNG: "Giám sát an ninh mạng", "Thẩm định an ninh mạng", "Kiểm tra, đánh giá an ninh mạng", "Quản lý rủi ro".
    SAI (nguyên câu, quá dài): "Có biện pháp, giải pháp để tìm và phát hiện kịp thời các điểm yếu, lỗ hổng về mặt kỹ thuật".
- Location: Địa điểm, địa danh cụ thể liên quan đến nội dung văn bản hoặc nơi ban hành văn bản.

## Quy tắc Composite Key cho Person (theo thứ tự ưu tiên):
1. "[Họ Tên] (DD/MM/YYYY)" — nếu có ngày sinh
2. "[Họ Tên] (Số CCCD)" — nếu có số CCCD/định danh cá nhân
3. "[Họ Tên] ([Đơn vị công tác rõ nhất])" — ví dụ: "Nguyễn Văn A (Sở Tài chính Nghệ An)"
4. "[Họ Tên] (không xác định)" — nếu không có thông tin định danh nào

## Quy tắc Canonicalization cho Organization:
- Các entity name được format không có các ký tự đặc biệt như: #, ?, *, ...
- Luôn dùng tên đầy đủ. Ví dụ: "UBND Tỉnh Nghệ An", không dùng "UBND tỉnh" hay "UBND"
- Sử dụng document_meta (thông tin văn bản) để suy diễn tên đầy đủ khi văn bản dùng tên tắt

## Các loại quan hệ được phép (PHẢI dùng chính xác tên sau):
- CAN_CU: Văn bản hiện tại căn cứ vào/dựa trên văn bản pháp lý khác. Source: Document → Target: Document
- VIEN_DAN: Điều khoản viện dẫn/tham chiếu một quy định khác. Source: Article → Target: Document/Article
- SUA_DOI: Văn bản sửa đổi, bổ sung văn bản khác (văn bản bị sửa VẪN còn hiệu lực). Source: Document → Target: Document
- THAY_THE: Văn bản thay thế TOÀN BỘ văn bản khác — văn bản bị thay thế HẾT hiệu lực. Nhận diện từ điều khoản thi hành: "thay thế Nghị định số...", "Luật X số ... hết hiệu lực kể từ ngày Luật này có hiệu lực". Source: Document → Target: Document
- BAI_BO: Văn bản bãi bỏ văn bản khác hoặc điều/khoản/điểm cụ thể của văn bản khác ("bãi bỏ Nghị định số...", "bãi bỏ khoản 3 Điều 49 của Luật..."). Source: Document → Target: Document/Article
- CHU_TRI: Đơn vị, cơ quan, cá nhân chủ trì thực hiện. Source: Task/Article → Target: Organization/Person
- PHOI_HOP: Đơn vị, cơ quan, cá nhân phối hợp thực hiện. Source: Task/Article → Target: Organization/Person
- CHIU_TRACH_NHIEM: Đơn vị chịu trách nhiệm thi hành hoặc giám sát. Source: Task/Article → Target: Organization/Person
- PART_OF: Điều/Khoản thuộc cấu trúc của văn bản. Source: Article → Target: Document
- REFERENCES: Điều/Khoản tham chiếu chung đến văn bản/điều khác. Source: Article → Target: Document/Article
- KY: Người ký ban hành văn bản. Source: Document → Target: Person

## QUY TẮC NGHIÊM NGẶT:
1. CHỈ trả về JSON hợp lệ, không có markdown, không có giải thích thêm.
2. KHÔNG được tạo ra bất kỳ loại quan hệ nào ngoài danh sách trên.
3. KHÔNG được tạo entity type ngoài danh sách trên.
4. XỬ LÝ TỰ THAM CHIẾU: TUYỆT ĐỐI KHÔNG trích xuất các cụm từ "quy định này", "quyết định này", "văn bản này" làm thực thể độc lập. Khi gặp câu "Điều X của quy định/quyết định này", BỎ QUA cụm từ chỉ văn bản, CHỈ lấy "Điều X" (loại Article). Nếu văn bản nói "Sở này", "cơ quan này", tìm ngữ cảnh trước đó để ghi TÊN ĐẦY ĐỦ.
5. LOẠI BỎ THỰC THỂ CHUNG CHUNG/RÁC: KHÔNG tạo entity cho tổ chức không định danh ("Bộ", "Cơ quan ngang Bộ", "Cơ quan nhà nước", "các … có liên quan"), placeholder biểu mẫu ("(tên đơn vị đề nghị)"), hay tên biểu mẫu ("Mẫu số 02", "Tờ trình"). Thà bỏ sót còn hơn tạo node rác. Nếu cần biểu diễn quan hệ tới một chủ thể chung chung, gắn quan hệ tới Article hoặc Organization định danh được trong ngữ cảnh, KHÔNG tạo node chung chung.
6. Document KHÔNG kèm hậu tố ngày ban hành. Task: VẪN trích xuất đầy đủ các nhiệm vụ chính, nhưng tên Task PHẢI rút gọn thành cụm động từ ngắn (3–6 từ), KHÔNG phải nguyên câu.
7. Nếu không trích xuất được gì, trả về: {"entities": [], "relations": []}
`

// LegalKGUserPrompt is a port of AIRAG LEGAL_KG_USER_PROMPT. Placeholders
// {document_title}, {document_number}, {issuing_agency}, {published_date},
// {article_text} are filled by renderLegalUserPrompt.
const LegalKGUserPrompt = `## Thông tin văn bản (document_meta)
Tiêu đề văn bản: "{document_title}"
Số hiệu: {document_number}
Cơ quan ban hành: {issuing_agency}
Ngày ban hành: {published_date}

Nội dung cần phân tích:
{article_text}

## Lưu ý quan trọng khi trích xuất:
- **Phân biệt Document vs Organization**: Tiêu đề văn bản (document_title) thường là TÊN ĐẦY ĐỦ của văn bản pháp luật (VD: "Luật Bảo vệ Bí mật nhà nước", "Kế hoạch triển khai"). Nếu entity trùng hoặc gần trùng với tiêu đề → đây là Document (văn bản), KHÔNG phải Organization.
- **Số hiệu**: Nếu entity chứa số hiệu văn bản (VD: "13/2024/QH15") → đây là Document.
- **Organization**: Là cơ quan/tổ chức CỤ THỂ được nhắc đến trong điều khoản, không phải tên văn bản.

Hãy trích xuất entities và relations theo đúng schema đã quy định.
Trả về JSON có dạng:
{
  "entities": [
    {"name": "...", "type": "Article|Document|Organization|Person|Task|Location", "description": "..."}
  ],
  "relations": [
    {"source": "...", "relation": "CAN_CU|VIEN_DAN|SUA_DOI|CHU_TRI|PHOI_HOP|CHIU_TRACH_NHIEM|PART_OF|REFERENCES|KY", "target": "...", "description": "..."}
  ]
}`

// PersonnelKGSystemPrompt is a port of AIRAG PERSON_EXTRACT_SYSTEM_PROMPT —
// the variant for personnel decisions (bổ nhiệm, điều động, kỷ luật…).
const PersonnelKGSystemPrompt = `Bạn là chuyên gia phân tích quyết định nhân sự trong văn bản hành chính Việt Nam.
Nhiệm vụ: Trích xuất thông tin cá nhân và quyết định liên quan từ văn bản hoặc điều khoản được cung cấp.

## Quy tắc Composite Key cho Person (PHẢI tuân thủ thứ tự ưu tiên):
1. "[Họ Tên] (DD/MM/YYYY)" — nếu có ngày sinh (chuẩn hóa về DD/MM/YYYY)
2. "[Họ Tên] (Số CCCD)" — nếu có số CCCD/định danh cá nhân
3. "[Họ Tên] ([Đơn vị công tác rõ nhất])" — ví dụ: "Nguyễn Văn A (Sở Tài chính Nghệ An)"
4. "[Họ Tên] (không xác định)" — nếu không có thông tin định danh nào

## Loại quan hệ Person:
- BO_NHIEM: Bổ nhiệm vào chức vụ mới
- MIEN_NHIEM: Miễn nhiệm khỏi chức vụ
- DIEU_DONG: Điều chuyển sang đơn vị khác
- NGHI_HUU: Nghỉ hưu theo chế độ
- KHEN_THUONG: Khen thưởng (bằng khen, huân chương...)
- KY_LUAT: Kỷ luật (cảnh cáo, khiển trách...)
- PHE_DUYET: Phê duyệt hồ sơ/đề án liên quan đến cá nhân
- LIEN_QUAN: Đề cập đến cá nhân trong điều khoản
- KY: Người ký ban hành văn bản

## QUY TẮC NGHIÊM NGẶT:
1. CHỈ trả về JSON hợp lệ, không có markdown, không có giải thích thêm.
2. XỬ LÝ TỰ THAM CHIẾU: TUYỆT ĐỐI KHÔNG trích xuất các cụm từ "quyết định này", "văn bản này" làm thực thể độc lập. Khi gặp "Điều X của quyết định này", CHỈ trích xuất "Điều X".
3. Nếu không trích xuất được gì, trả về: {"entities": [], "relations": []}
`

// PersonnelKGUserPrompt is a port of AIRAG PERSON_EXTRACT_USER_PROMPT.
const PersonnelKGUserPrompt = `## Thông tin văn bản (document_meta)
Tiêu đề văn bản: "{document_title}"
Số hiệu: {document_number}
Cơ quan ban hành: {issuing_agency}
Ngày ban hành: {published_date}

Nội dung cần phân tích:
{article_text}

Trả về JSON theo dạng:
{
  "entities": [
    {"name": "[Họ Tên] (Composite Key)", "type": "Person", "description": "..."}
  ],
  "relations": [
    {
      "source": "Tên văn bản/điều khoản",
      "relation": "BO_NHIEM|MIEN_NHIEM|DIEU_DONG|NGHI_HUU|KHEN_THUONG|KY_LUAT|PHE_DUYET|LIEN_QUAN|KY",
      "target": "[Họ Tên] (Composite Key)",
      "description": "Mô tả tổng hợp bắt buộc"
    }
  ]
}`

// legalEntity / legalRelation / legalExtraction decode the LLM's JSON
// payload ({entities, relations}) — the schema both legal prompts emit.
type legalEntity struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Description string `json:"description"`
}

type legalRelation struct {
	Source      string `json:"source"`
	Relation    string `json:"relation"`
	Target      string `json:"target"`
	Description string `json:"description"`
}

type legalExtraction struct {
	Entities  []legalEntity   `json:"entities"`
	Relations []legalRelation `json:"relations"`
}

// LegalGraphExtractor runs one LLM extraction call with the legal prompt
// pair selected by the document context (personnel vs general).
type LegalGraphExtractor struct {
	chat    chat.Chat
	chatOpt *chat.ChatOptions
}

// NewLegalGraphExtractor creates the legal extractor. temperature=0 mirrors
// AIRAG's extraction settings; Thinking disabled keeps output pure JSON.
func NewLegalGraphExtractor(chatModel chat.Chat) *LegalGraphExtractor {
	think := false
	return &LegalGraphExtractor{
		chat: chatModel,
		chatOpt: &chat.ChatOptions{
			Temperature: 0.0,
			MaxTokens:   4096,
			Thinking:    &think,
		},
	}
}

// renderLegalUserPrompt fills the document_meta placeholders. Placeholders
// use {name} tokens replaced by simple substitution so literal JSON braces
// in the template cannot collide with fmt formatting.
func renderLegalUserPrompt(template string, doc *vietnamese_legal.LegalDocContext, articleText string) string {
	return strings.NewReplacer(
		"{document_title}", doc.Title,
		"{document_number}", doc.DocumentNumber,
		"{issuing_agency}", doc.IssuingAgency,
		"{published_date}", doc.PublishedDate,
		"{article_text}", articleText,
	).Replace(template)
}

// Extract calls the chat model once and decodes the {entities, relations}
// payload into a GraphData. Node.Type carries the raw LLM type so
// PostProcessLegalGraph can canonicalize it deterministically.
func (e *LegalGraphExtractor) Extract(
	ctx context.Context,
	doc *vietnamese_legal.LegalDocContext,
	chunkContent string,
	customInstructions string,
) (*types.GraphData, error) {
	systemPrompt, userTemplate := LegalKGSystemPrompt, LegalKGUserPrompt
	if doc.IsPersonnel {
		systemPrompt, userTemplate = PersonnelKGSystemPrompt, PersonnelKGUserPrompt
	}
	systemPrompt = types.AppendCustomPromptInstructions(systemPrompt, customInstructions, "graph_extraction")

	modelCtx := types.WithLLMCallMetadata(ctx, "legal_entity_extraction", "")
	resp, err := e.chat.Chat(modelCtx, []chat.Message{
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: renderLegalUserPrompt(userTemplate, doc, chunkContent)},
	}, e.chatOpt)
	if err != nil {
		logger.Errorf(ctx, "legal extraction chat failed: %v", err)
		return nil, err
	}

	var out legalExtraction
	if err := common.ParseLLMJsonResponse(resp.Content, &out); err != nil {
		logger.Errorf(ctx, "failed to parse legal extraction JSON: %v", err)
		return nil, err
	}

	graph := &types.GraphData{Text: chunkContent}
	for _, ent := range out.Entities {
		node := &types.GraphNode{
			Name: ent.Name,
			Type: ent.Type,
		}
		if d := strings.TrimSpace(ent.Description); d != "" {
			node.Attributes = []string{d}
		}
		graph.Node = append(graph.Node, node)
	}
	for _, rel := range out.Relations {
		graph.Relation = append(graph.Relation, &types.GraphRelation{
			Node1: rel.Source,
			Node2: rel.Target,
			Type:  rel.Relation,
		})
	}
	return graph, nil
}

// ---------------------------------------------------------------------------
// Deterministic post-processing (port of LegalKGService._basic_entity_normalize
// + _build_canonical_lookup + the _store_extraction relation guards)
// ---------------------------------------------------------------------------

// PostProcessLegalGraph canonicalizes a per-chunk extraction:
//
//  1. Canonicalize every entity name via ForceLegalType + NormalizeEntityID
//     (số hiệu becomes the Document merge key, organizations get canonical
//     casing) and dedupe on the canonical form.
//  2. Fold self-references ("văn bản này", same số hiệu, title aliases) onto
//     the document root node instead of duplicating them.
//  3. Drop generic/junk entities so they never materialize as nodes — and
//     drop relations whose endpoints resolve to junk.
//  4. Reroute relation endpoints through the canonical map and drop
//     self-loops and relations with non-legal types.
//  5. Synthesize the structural edges: Article→PART_OF→root,
//     root→BAN_HANH_BOI→issuing agency, and root→CAN_CU→preamble refs
//     (CAN_CU only when includeCanCu — the header chunk's task).
//
// The function is idempotent on canonical input and never needs an LLM.
func PostProcessLegalGraph(
	ctx context.Context,
	g *types.GraphData,
	doc *vietnamese_legal.LegalDocContext,
	includeCanCu bool,
) *types.GraphData {
	if g == nil {
		return &types.GraphData{}
	}
	out := &types.GraphData{Text: g.Text}

	nodeByKey := map[string]*types.GraphNode{}
	dropped := map[string]bool{}

	// canonicalOf maps a raw extracted name to its canonical node key.
	canonicalOf := map[string]string{}
	nodeKey := func(name, etype string) string { return etype + "|" + name }

	addNode := func(name, etype string, attrs []string) *types.GraphNode {
		key := nodeKey(name, etype)
		if n, ok := nodeByKey[key]; ok {
			n.Attributes = mergeAttributes(n.Attributes, attrs)
			return n
		}
		n := &types.GraphNode{Name: name, Type: etype, Attributes: attrs}
		nodeByKey[key] = n
		out.Node = append(out.Node, n)
		return n
	}

	// resolveEndpoint maps any raw name onto the canonical node it refers
	// to (document root on self-refs) and creates the stub when missing.
	resolveEndpoint := func(raw string) (*types.GraphNode, bool) {
		name := vietnamese_legal.CleanEntityName(raw)
		if name == "" {
			return nil, false
		}
		if doc.RootName != "" &&
			(vietnamese_legal.IsSelfReferenceName(name) ||
				vietnamese_legal.IsDocRootAlias(name, doc.RootName, doc.Title)) {
			return addNode(doc.RootName, vietnamese_legal.EntityTypeDocument, nil), true
		}
		if canonical, ok := canonicalOf[raw]; ok {
			if dropped[raw] {
				return nil, false
			}
			// Find the node we recorded for this raw name.
			for _, n := range out.Node {
				if n.Name == canonical {
					return n, true
				}
			}
			return nil, false
		}
		if dropped[raw] {
			return nil, false
		}
		etype := vietnamese_legal.ForceLegalType(name, "")
		if !vietnamese_legal.IsLegalEntityType(etype) {
			etype = vietnamese_legal.EntityTypeOrganization
		}
		if vietnamese_legal.IsGenericOrJunkEntity(name, etype) {
			dropped[raw] = true
			return nil, false
		}
		canonical := vietnamese_legal.NormalizeEntityID(name, etype)
		return addNode(canonical, etype, nil), true
	}

	// 1. Canonicalize entities.
	for _, node := range g.Node {
		if node == nil {
			continue
		}
		raw := node.Name
		name := vietnamese_legal.CleanEntityName(raw)
		if name == "" {
			dropped[raw] = true
			continue
		}
		// Self-references fold onto the document root — no separate node.
		if doc.RootName != "" &&
			(vietnamese_legal.IsSelfReferenceName(name) ||
				vietnamese_legal.IsDocRootAlias(name, doc.RootName, doc.Title)) {
			canonicalOf[raw] = doc.RootName
			addNode(doc.RootName, vietnamese_legal.EntityTypeDocument, nil)
			continue
		}
		etype := vietnamese_legal.ForceLegalType(name, node.Type)
		if !vietnamese_legal.IsLegalEntityType(etype) {
			etype = vietnamese_legal.EntityTypeOrganization
		}
		if vietnamese_legal.IsGenericOrJunkEntity(name, etype) {
			dropped[raw] = true
			continue
		}
		canonical := vietnamese_legal.NormalizeEntityID(name, etype)
		if etype == vietnamese_legal.EntityTypePerson &&
			!strings.Contains(canonical, "(") {
			canonical += " (không xác định)"
		}
		canonicalOf[raw] = canonical
		addNode(canonical, etype, node.Attributes)
	}

	// 2. Reroute + validate relations.
	for _, rel := range g.Relation {
		if rel == nil {
			continue
		}
		relType := strings.ToUpper(strings.TrimSpace(rel.Type))
		if !vietnamese_legal.IsLegalRelationType(relType) {
			logger.Warnf(ctx, "legal graph: dropping unknown relation type %q", rel.Type)
			continue
		}
		src, ok1 := resolveEndpoint(rel.Node1)
		tgt, ok2 := resolveEndpoint(rel.Node2)
		if !ok1 || !ok2 {
			continue
		}
		if src.Name == tgt.Name && src.Type == tgt.Type {
			continue // self-loop
		}
		out.Relation = append(out.Relation, &types.GraphRelation{
			Node1: src.Name,
			Node2: tgt.Name,
			Type:  relType,
		})
	}

	// 3. Structural edges. The document root always exists once a legal
	// document is processed — AIRAG creates it explicitly at ingest.
	if doc.RootName != "" {
		root := addNode(doc.RootName, vietnamese_legal.EntityTypeDocument, nil)
		for _, n := range out.Node {
			if n != root && n.Type == vietnamese_legal.EntityTypeArticle {
				out.Relation = append(out.Relation, &types.GraphRelation{
					Node1: n.Name, Node2: root.Name, Type: vietnamese_legal.RelPartOf,
				})
			}
		}
		if doc.IssuingAgency != "" {
			org := addNode(
				vietnamese_legal.NormalizeEntityID(doc.IssuingAgency, vietnamese_legal.EntityTypeOrganization),
				vietnamese_legal.EntityTypeOrganization, nil)
			if org.Name != root.Name {
				out.Relation = append(out.Relation, &types.GraphRelation{
					Node1: root.Name, Node2: org.Name, Type: vietnamese_legal.RelBanHanhBoi,
				})
			}
		}
		if includeCanCu {
			for _, ref := range doc.CanCu {
				refName := strings.TrimSpace(ref)
				if refName == "" || vietnamese_legal.IsDocRootAlias(refName, doc.RootName, doc.Title) {
					continue
				}
				etype := vietnamese_legal.EntityTypeDocument
				if vietnamese_legal.IsArticleRefName(refName) {
					etype = vietnamese_legal.EntityTypeArticle
				}
				target := addNode(
					vietnamese_legal.NormalizeEntityID(refName, etype), etype, nil)
				if target.Name != root.Name {
					out.Relation = append(out.Relation, &types.GraphRelation{
						Node1: root.Name, Node2: target.Name, Type: vietnamese_legal.RelCanCu,
					})
				}
			}
		}
	}

	return out
}

// mergeAttributes unions two attribute lists preserving order.
func mergeAttributes(a, b []string) []string {
	if len(b) == 0 {
		return a
	}
	seen := make(map[string]bool, len(a))
	for _, s := range a {
		seen[s] = true
	}
	out := a
	for _, s := range b {
		if !seen[s] {
			out = append(out, s)
		}
	}
	return out
}

// legalGraphDataFromJSON is the exported decode helper kept for tests and
// for callers that receive the raw LLM payload.
func legalGraphDataFromJSON(ctx context.Context, payload string) (*types.GraphData, error) {
	var out legalExtraction
	if err := json.Unmarshal([]byte(payload), &out); err != nil {
		return nil, err
	}
	graph := &types.GraphData{Text: payload}
	for _, ent := range out.Entities {
		node := &types.GraphNode{Name: ent.Name, Type: ent.Type}
		if d := strings.TrimSpace(ent.Description); d != "" {
			node.Attributes = []string{d}
		}
		graph.Node = append(graph.Node, node)
	}
	for _, rel := range out.Relations {
		graph.Relation = append(graph.Relation, &types.GraphRelation{
			Node1: rel.Source, Node2: rel.Target, Type: rel.Relation,
		})
	}
	return graph, nil
}
