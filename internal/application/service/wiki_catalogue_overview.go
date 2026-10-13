package service

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

// WikiCatalogueOverviewService writes a knowledge base's wiki from structured
// data instead of the LLM pipeline: one page per legal document (metadata,
// the summary already produced at ingest, and its catalogue relations) plus
// index pages per lĩnh vực and per cơ quan ban hành, and a root index. It is
// the implementation behind WikiConfig.GenerationMode == "catalogue".
//
// Rebuilds are idempotent: pages are keyed by slug, unchanged pages are
// skipped via a content hash in page_metadata, and generated pages whose
// document left the KB are deleted. No LLM call is made at any point.
type WikiCatalogueOverviewService struct {
	wiki          interfaces.WikiPageService
	kbService     interfaces.KnowledgeBaseService
	knowledgeRepo interfaces.KnowledgeRepository
	chunkRepo     interfaces.ChunkRepository
	catalog       interfaces.LegalCatalogRepository

	mu     sync.Mutex
	status map[string]*WikiCatalogueBuildStatus
}

// WikiCatalogueBuildStatus is the progress of the latest rebuild of one KB.
type WikiCatalogueBuildStatus struct {
	KnowledgeBaseID string     `json:"knowledge_base_id"`
	Running         bool       `json:"running"`
	Phase           string     `json:"phase"`
	Total           int        `json:"total"`
	Done            int        `json:"done"`
	Created         int        `json:"created"`
	Updated         int        `json:"updated"`
	Skipped         int        `json:"skipped"`
	Deleted         int        `json:"deleted"`
	Failed          int        `json:"failed"`
	Error           string     `json:"error,omitempty"`
	StartedAt       time.Time  `json:"started_at"`
	FinishedAt      *time.Time `json:"finished_at,omitempty"`
}

// ErrWikiCatalogueBuildRunning is returned when a rebuild is already in flight.
var ErrWikiCatalogueBuildRunning = errors.New("a catalogue wiki rebuild is already running for this knowledge base")

// ErrWikiCatalogueModeRequired is returned when the KB's wiki is not in catalogue mode.
var ErrWikiCatalogueModeRequired = errors.New("wiki generation mode must be \"catalogue\" and the wiki must be enabled")

const wikiCatalogueGenerator = "vbpl-catalogue"

func NewWikiCatalogueOverviewService(
	wiki interfaces.WikiPageService,
	kbService interfaces.KnowledgeBaseService,
	knowledgeRepo interfaces.KnowledgeRepository,
	chunkRepo interfaces.ChunkRepository,
	catalog interfaces.LegalCatalogRepository,
) *WikiCatalogueOverviewService {
	return &WikiCatalogueOverviewService{
		wiki:          wiki,
		kbService:     kbService,
		knowledgeRepo: knowledgeRepo,
		chunkRepo:     chunkRepo,
		catalog:       catalog,
		status:        map[string]*WikiCatalogueBuildStatus{},
	}
}

// Status returns the latest build status for the KB, or nil when none ran.
func (s *WikiCatalogueOverviewService) Status(kbID string) *WikiCatalogueBuildStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	st, ok := s.status[kbID]
	if !ok {
		return nil
	}
	cp := *st
	return &cp
}

// StartRebuild validates the KB and launches the rebuild in the background.
// The returned status is the initial snapshot.
func (s *WikiCatalogueOverviewService) StartRebuild(ctx context.Context, kbID string) (*WikiCatalogueBuildStatus, error) {
	kb, err := s.kbService.GetKnowledgeBaseByID(ctx, kbID)
	if err != nil {
		return nil, err
	}
	if kb == nil || !kb.IsWikiEnabled() || !kb.WikiConfig.IsCatalogueMode() {
		return nil, ErrWikiCatalogueModeRequired
	}

	s.mu.Lock()
	if st, ok := s.status[kbID]; ok && st.Running {
		s.mu.Unlock()
		return nil, ErrWikiCatalogueBuildRunning
	}
	st := &WikiCatalogueBuildStatus{KnowledgeBaseID: kbID, Running: true, Phase: "starting", StartedAt: time.Now()}
	s.status[kbID] = st
	s.mu.Unlock()

	bg := context.WithValue(context.Background(), types.TenantIDContextKey, kb.TenantID)
	bg = types.WithWikiEditSource(bg, types.WikiEditSourcePipeline)
	go func() {
		err := s.rebuild(bg, kb)
		s.mu.Lock()
		defer s.mu.Unlock()
		now := time.Now()
		st.Running = false
		st.FinishedAt = &now
		if err != nil {
			st.Error = err.Error()
			st.Phase = "failed"
			logger.Errorf(bg, "[WikiCatalogue] rebuild for KB %s failed: %v", kbID, err)
		} else {
			st.Phase = "done"
			logger.Infof(bg, "[WikiCatalogue] rebuild for KB %s done: %d pages (created %d, updated %d, skipped %d, deleted %d, failed %d)",
				kbID, st.Total, st.Created, st.Updated, st.Skipped, st.Deleted, st.Failed)
			if st.Failed > 0 {
				st.Error = fmt.Sprintf("%d pages failed to write; see server log", st.Failed)
			}
		}
	}()
	cp := *st
	return &cp, nil
}

func (s *WikiCatalogueOverviewService) setPhase(kbID, phase string, total int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if st, ok := s.status[kbID]; ok {
		st.Phase = phase
		if total >= 0 {
			st.Total = total
		}
	}
}

func (s *WikiCatalogueOverviewService) bump(kbID string, f func(st *WikiCatalogueBuildStatus)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if st, ok := s.status[kbID]; ok {
		f(st)
	}
}

// catalogueDoc is one document of the KB joined with its catalogue row.
type catalogueDoc struct {
	Knowledge *types.Knowledge
	Doc       *types.LegalDocument
	Slug      string
	Fields    []string                         // lĩnh vực, split from the catalogue's comma-joined field
	Summary   string                           // plain paragraphs, "# Summary" heading stripped
	Outgoing  map[types.LegalRelation][]string // relation → related document IDs
	Incoming  map[types.LegalRelation][]string
}

// cataloguePage is a page about to be written.
type cataloguePage struct {
	Slug       string
	Title      string
	PageType   string
	Summary    string
	Content    string
	FolderPath []string
	SourceRefs []string
	Meta       map[string]any
}

func (s *WikiCatalogueOverviewService) rebuild(ctx context.Context, kb *types.KnowledgeBase) error {
	kbID := kb.ID
	s.setPhase(kbID, "loading documents", -1)

	knowledges, err := s.knowledgeRepo.ListKnowledgeByKnowledgeBaseID(ctx, kb.TenantID, kbID)
	if err != nil {
		return fmt.Errorf("list knowledge: %w", err)
	}
	docs := make([]*catalogueDoc, 0, len(knowledges))
	byID := map[string]*catalogueDoc{}
	ids := make([]string, 0, len(knowledges))
	for _, k := range knowledges {
		if k == nil || k.ParseStatus != "completed" {
			continue
		}
		id := vbplSourceDocID(k)
		if id == "" || byID[id] != nil {
			continue
		}
		d := &catalogueDoc{Knowledge: k, Outgoing: map[types.LegalRelation][]string{}, Incoming: map[types.LegalRelation][]string{}}
		docs = append(docs, d)
		byID[id] = d
		ids = append(ids, id)
	}
	if len(docs) == 0 {
		return errors.New("no processed vbpl documents in this knowledge base")
	}

	// Catalogue rows for the KB's own documents.
	rows, err := s.catalogueByIDs(ctx, ids)
	if err != nil {
		return err
	}
	for _, d := range docs {
		d.Doc = rows[vbplSourceDocID(d.Knowledge)]
		if d.Doc == nil {
			d.Doc = catalogueRowFromKnowledge(d.Knowledge)
		}
		d.Slug = catalogueDocSlug(d.Doc)
	}

	// Relations in both directions, then the catalogue rows of the other ends.
	s.setPhase(kbID, "loading relations", -1)
	relIDs := map[string]bool{}
	for _, incoming := range []bool{false, true} {
		for start := 0; start < len(ids); start += 500 {
			end := start + 500
			if end > len(ids) {
				end = len(ids)
			}
			edges, err := s.catalog.ListEdges(ctx, ids[start:end], catalogueRelationOrder, incoming)
			if err != nil {
				return fmt.Errorf("list relations: %w", err)
			}
			for _, e := range edges {
				if e.FromID == e.ToID {
					continue
				}
				if incoming {
					if d := byID[e.ToID]; d != nil {
						d.Incoming[e.Relation] = append(d.Incoming[e.Relation], e.FromID)
						relIDs[e.FromID] = true
					}
				} else if d := byID[e.FromID]; d != nil {
					d.Outgoing[e.Relation] = append(d.Outgoing[e.Relation], e.ToID)
					relIDs[e.ToID] = true
				}
			}
		}
	}
	otherIDs := make([]string, 0, len(relIDs))
	for id := range relIDs {
		if byID[id] == nil && rows[id] == nil {
			otherIDs = append(otherIDs, id)
		}
	}
	others, err := s.catalogueByIDs(ctx, otherIDs)
	if err != nil {
		return err
	}
	for id, r := range others {
		rows[id] = r
	}
	for _, d := range docs {
		d.Fields = splitFields(d.Doc.Field)
	}

	// Summaries produced at ingest.
	s.setPhase(kbID, "loading summaries", -1)
	for _, d := range docs {
		chunks, err := s.chunkRepo.ListChunksByKnowledgeIDAndTypes(ctx, kb.TenantID, d.Knowledge.ID, []types.ChunkType{types.ChunkTypeSummary})
		if err != nil {
			return fmt.Errorf("load summary of %s: %w", d.Knowledge.ID, err)
		}
		for _, c := range chunks {
			if c != nil && strings.TrimSpace(c.Content) != "" {
				d.Summary = stripSummaryHeading(c.Content)
				break
			}
		}
	}

	// Render.
	s.setPhase(kbID, "rendering", -1)
	pages := renderCataloguePages(kb, docs, rows)
	s.setPhase(kbID, "writing pages", len(pages))

	// Write document pages first so index pages link to existing targets.
	wanted := map[string]bool{}
	folderIDs := map[string]string{}
	for _, p := range pages {
		wanted[p.Slug] = true
		if err := s.upsertPage(ctx, kb, p, folderIDs); err != nil {
			logger.Warnf(ctx, "[WikiCatalogue] write page %s: %v", p.Slug, err)
			s.bump(kbID, func(st *WikiCatalogueBuildStatus) { st.Failed++; st.Done++ })
			continue
		}
		s.bump(kbID, func(st *WikiCatalogueBuildStatus) { st.Done++ })
	}

	// Remove generated pages whose document is gone.
	s.setPhase(kbID, "pruning", -1)
	deleted, err := s.pruneStalePages(ctx, kbID, wanted)
	if err != nil {
		return err
	}
	s.bump(kbID, func(st *WikiCatalogueBuildStatus) { st.Deleted = deleted })

	s.setPhase(kbID, "rebuilding links", -1)
	if err := s.wiki.RebuildLinks(ctx, kbID); err != nil {
		logger.Warnf(ctx, "[WikiCatalogue] rebuild links for KB %s: %v", kbID, err)
	}
	return nil
}

func (s *WikiCatalogueOverviewService) catalogueByIDs(ctx context.Context, ids []string) (map[string]*types.LegalDocument, error) {
	out := map[string]*types.LegalDocument{}
	for start := 0; start < len(ids); start += 500 {
		end := start + 500
		if end > len(ids) {
			end = len(ids)
		}
		rows, err := s.catalog.GetByIDs(ctx, ids[start:end])
		if err != nil {
			return nil, fmt.Errorf("load catalogue rows: %w", err)
		}
		for _, r := range rows {
			if r != nil {
				out[r.ID] = r
			}
		}
	}
	return out, nil
}

// upsertPage creates or updates one page, skipping it when the stored hash
// matches. folderIDs caches resolved folder paths across pages.
func (s *WikiCatalogueOverviewService) upsertPage(ctx context.Context, kb *types.KnowledgeBase, p *cataloguePage, folderIDs map[string]string) error {
	folderID := ""
	if len(p.FolderPath) > 0 {
		key := strings.Join(p.FolderPath, "\x00")
		id, ok := folderIDs[key]
		if !ok {
			var err error
			id, _, err = s.wiki.FindOrCreateFolderPath(ctx, kb.ID, kb.TenantID, p.FolderPath)
			if err != nil {
				return fmt.Errorf("folder %v: %w", p.FolderPath, err)
			}
			folderIDs[key] = id
		}
		folderID = id
	}

	hash := cataloguePageHash(p, folderID)
	meta := map[string]any{}
	for k, v := range p.Meta {
		meta[k] = v
	}
	meta["generator"] = wikiCatalogueGenerator
	meta["content_hash"] = hash
	metaJSON, _ := json.Marshal(meta)

	existing, err := s.wiki.GetPageBySlug(ctx, kb.ID, p.Slug)
	switch {
	case err == nil && existing != nil:
		if existingHash(existing) == hash && existing.Status == types.WikiPageStatusPublished {
			s.bump(kb.ID, func(st *WikiCatalogueBuildStatus) { st.Skipped++ })
			return nil
		}
		page := *existing
		page.Title = p.Title
		page.PageType = p.PageType
		page.Summary = p.Summary
		page.Content = p.Content
		page.Status = types.WikiPageStatusPublished
		page.SourceRefs = nonNilStrings(p.SourceRefs)
		page.ChunkRefs = types.StringArray{}
		page.Aliases = nonNilStrings(page.Aliases)
		page.PageMetadata = types.JSON(metaJSON)
		if _, err := s.wiki.UpdatePage(ctx, &page); err != nil {
			return err
		}
		if existing.FolderID != folderID {
			if _, err := s.wiki.MovePage(ctx, kb.ID, p.Slug, folderID); err != nil {
				return fmt.Errorf("move to folder: %w", err)
			}
		}
		s.bump(kb.ID, func(st *WikiCatalogueBuildStatus) { st.Updated++ })
		return nil
	case err != nil && !errors.Is(err, repository.ErrWikiPageNotFound):
		return err
	}

	page := &types.WikiPage{
		TenantID:        kb.TenantID,
		KnowledgeBaseID: kb.ID,
		Slug:            p.Slug,
		Title:           p.Title,
		PageType:        p.PageType,
		Status:          types.WikiPageStatusPublished,
		Summary:         p.Summary,
		Content:         p.Content,
		FolderID:        folderID,
		SourceRefs:      nonNilStrings(p.SourceRefs),
		ChunkRefs:       types.StringArray{},
		Aliases:         types.StringArray{},
		PageMetadata:    types.JSON(metaJSON),
	}
	if _, err := s.wiki.CreatePage(ctx, page); err != nil {
		return err
	}
	s.bump(kb.ID, func(st *WikiCatalogueBuildStatus) { st.Created++ })
	return nil
}

// nonNilStrings returns an empty (not nil) array so JSON clients see [] and
// never null for list fields.
func nonNilStrings(v []string) types.StringArray {
	if len(v) == 0 {
		return types.StringArray{}
	}
	return types.StringArray(v)
}

func existingHash(page *types.WikiPage) string {
	if page == nil || len(page.PageMetadata) == 0 {
		return ""
	}
	var m struct {
		Generator string `json:"generator"`
		Hash      string `json:"content_hash"`
	}
	if err := json.Unmarshal(page.PageMetadata, &m); err != nil || m.Generator != wikiCatalogueGenerator {
		return ""
	}
	return m.Hash
}

func cataloguePageHash(p *cataloguePage, folderID string) string {
	h := sha1.New()
	for _, part := range []string{p.Title, p.PageType, p.Summary, p.Content, folderID, strings.Join(p.SourceRefs, ",")} {
		h.Write([]byte(part))
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}

// pruneStalePages deletes catalogue-generated pages not in wanted.
func (s *WikiCatalogueOverviewService) pruneStalePages(ctx context.Context, kbID string, wanted map[string]bool) (int, error) {
	deleted := 0
	cursor := ""
	for {
		pages, next, err := s.wiki.ListPagesCursor(ctx, kbID, cursor, 500)
		if err != nil {
			return deleted, fmt.Errorf("list pages: %w", err)
		}
		for _, page := range pages {
			if page == nil || wanted[page.Slug] || existingHash(page) == "" {
				continue
			}
			if err := s.wiki.DeletePage(ctx, kbID, page.Slug); err != nil {
				logger.Warnf(ctx, "[WikiCatalogue] delete stale page %s: %v", page.Slug, err)
				continue
			}
			deleted++
		}
		if next == "" || len(pages) == 0 {
			return deleted, nil
		}
		cursor = next
	}
}

// ---------------------------------------------------------------------------
// Rendering (pure functions; covered by unit tests)
// ---------------------------------------------------------------------------

const (
	catalogueDocPrefix    = "van-ban/"
	catalogueFieldPrefix  = "linh-vuc/"
	catalogueIssuerPrefix = "co-quan/"
	catalogueFieldHub     = "linh-vuc"
	catalogueIssuerHub    = "co-quan"
	catalogueRootSlug     = "index"
	catalogueNoField      = "Chưa phân loại"
	catalogueNoIssuer     = "Chưa rõ cơ quan ban hành"

	catalogueMaxTitleRunes   = 160
	catalogueMaxMinorLinks   = 30  // based_on / references lists are capped
	catalogueMaxMajorLinks   = 200 // amends / replaces / guides … lists
	catalogueRootTopFields   = 30
	catalogueMaxIndexEntries = 2000
)

// catalogueRelationOrder is the display order of relation groups.
var catalogueRelationOrder = []types.LegalRelation{
	types.LegalRelationAmends,
	types.LegalRelationReplaces,
	types.LegalRelationRepeals,
	types.LegalRelationExpires,
	types.LegalRelationPartiallyExpires,
	types.LegalRelationSuspends,
	types.LegalRelationCorrects,
	types.LegalRelationConsolidates,
	types.LegalRelationGuides,
	types.LegalRelationInterprets,
	types.LegalRelationBasedOn,
	types.LegalRelationReferences,
	types.LegalRelationRelated,
}

// Labels seen from the page's document: outgoing = "this document <verb>
// the other", incoming = "the other <verb> this document".
var catalogueOutgoingLabel = map[types.LegalRelation]string{
	types.LegalRelationAmends:           "Sửa đổi, bổ sung",
	types.LegalRelationReplaces:         "Thay thế",
	types.LegalRelationRepeals:          "Bãi bỏ",
	types.LegalRelationExpires:          "Làm hết hiệu lực",
	types.LegalRelationPartiallyExpires: "Làm hết hiệu lực một phần",
	types.LegalRelationSuspends:         "Đình chỉ",
	types.LegalRelationCorrects:         "Đính chính",
	types.LegalRelationConsolidates:     "Hợp nhất",
	types.LegalRelationGuides:           "Hướng dẫn, quy định chi tiết",
	types.LegalRelationInterprets:       "Giải thích",
	types.LegalRelationBasedOn:          "Căn cứ ban hành",
	types.LegalRelationReferences:       "Dẫn chiếu",
	types.LegalRelationRelated:          "Liên quan",
}

var catalogueIncomingLabel = map[types.LegalRelation]string{
	types.LegalRelationAmends:           "Được sửa đổi, bổ sung bởi",
	types.LegalRelationReplaces:         "Bị thay thế bởi",
	types.LegalRelationRepeals:          "Bị bãi bỏ bởi",
	types.LegalRelationExpires:          "Hết hiệu lực bởi",
	types.LegalRelationPartiallyExpires: "Hết hiệu lực một phần bởi",
	types.LegalRelationSuspends:         "Bị đình chỉ bởi",
	types.LegalRelationCorrects:         "Được đính chính bởi",
	types.LegalRelationConsolidates:     "Được hợp nhất bởi",
	types.LegalRelationGuides:           "Được hướng dẫn, quy định chi tiết bởi",
	types.LegalRelationInterprets:       "Được giải thích bởi",
	types.LegalRelationBasedOn:          "Là căn cứ ban hành của",
	types.LegalRelationReferences:       "Được dẫn chiếu bởi",
	types.LegalRelationRelated:          "Liên quan",
}

func catalogueRelationCap(rel types.LegalRelation) int {
	switch rel {
	case types.LegalRelationBasedOn, types.LegalRelationReferences, types.LegalRelationRelated:
		return catalogueMaxMinorLinks
	}
	return catalogueMaxMajorLinks
}

// catalogueStatusOrder groups index entries; unknown statuses go last.
var catalogueStatusOrder = []string{"Còn hiệu lực", "Chưa có hiệu lực", "Hết hiệu lực một phần", "Ngưng hiệu lực", "Hết hiệu lực toàn bộ"}

// vnSlug transliterates Vietnamese to an ASCII slug ("Thuế, phí" → "thue-phi").
func vnSlug(s string) string {
	s = strings.TrimSpace(s)
	s = strings.NewReplacer("đ", "d", "Đ", "D").Replace(s)
	s = norm.NFD.String(s)
	var b strings.Builder
	lastDash := true
	for _, r := range strings.ToLower(s) {
		if unicode.Is(unicode.Mn, r) {
			continue
		}
		switch {
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'):
			b.WriteRune(r)
			lastDash = false
		default:
			if !lastDash {
				b.WriteByte('-')
				lastDash = true
			}
		}
	}
	out := strings.Trim(b.String(), "-")
	if len(out) > 120 {
		out = strings.Trim(out[:120], "-")
	}
	return out
}

func catalogueDocSlug(doc *types.LegalDocument) string {
	num := vnSlug(doc.DocumentNumber)
	if num == "" {
		num = "van-ban"
	}
	return catalogueDocPrefix + num + "-" + vnSlug(doc.ID)
}

func catalogueFieldSlug(field string) string {
	if strings.TrimSpace(field) == "" {
		field = catalogueNoField
	}
	return catalogueFieldPrefix + vnSlug(field)
}

func catalogueIssuerSlug(issuer string) string {
	if strings.TrimSpace(issuer) == "" {
		issuer = catalogueNoIssuer
	}
	return catalogueIssuerPrefix + vnSlug(issuer)
}

func catalogueRowFromKnowledge(k *types.Knowledge) *types.LegalDocument {
	doc := &types.LegalDocument{ID: vbplSourceDocID(k), Title: k.Title}
	if m, err := k.Metadata.Map(); err == nil {
		doc.DocumentNumber, _ = m["doc_number"].(string)
		doc.DocType, _ = m["doc_type"].(string)
		doc.Status, _ = m["eff_status"].(string)
	}
	return doc
}

func stripSummaryHeading(content string) string {
	content = strings.TrimSpace(content)
	for _, h := range []string{"# Summary", "# Tóm tắt", "## Summary", "## Tóm tắt"} {
		if strings.HasPrefix(content, h) {
			content = strings.TrimSpace(strings.TrimPrefix(content, h))
			break
		}
	}
	return content
}

func vnDate(t *time.Time) string {
	if t == nil || t.IsZero() {
		return ""
	}
	return t.Format("02/01/2006")
}

func mdEscape(s string) string {
	s = strings.ReplaceAll(s, "|", "\\|")
	s = strings.ReplaceAll(s, "[[", "[ [")
	return strings.TrimSpace(s)
}

func clipRunes(s string, max int) string {
	r := []rune(strings.TrimSpace(s))
	if len(r) <= max {
		return string(r)
	}
	return strings.TrimSpace(string(r[:max])) + "…"
}

func vbplDocURL(id string) string {
	return "https://vbpl.vn/van-ban/chi-tiet/" + id
}

func docDisplayNumber(doc *types.LegalDocument) string {
	if n := strings.TrimSpace(doc.DocumentNumber); n != "" {
		return n
	}
	return "Văn bản " + doc.ID
}

func docHeading(doc *types.LegalDocument) string {
	t := strings.TrimSpace(doc.DocType)
	n := docDisplayNumber(doc)
	if t == "" {
		return n
	}
	if strings.HasPrefix(strings.ToLower(n), strings.ToLower(t)) {
		return n
	}
	return t + " " + n
}

// docLink renders a link to a related document: an in-wiki link when the
// document is in this KB, otherwise a vbpl.vn link.
func docLink(id string, byID map[string]*catalogueDoc, rows map[string]*types.LegalDocument) string {
	if d := byID[id]; d != nil {
		return fmt.Sprintf("[[%s|%s]] — %s", d.Slug, mdEscape(docDisplayNumber(d.Doc)), mdEscape(clipRunes(d.Doc.Title, catalogueMaxTitleRunes)))
	}
	if r := rows[id]; r != nil {
		label := docDisplayNumber(r)
		if r.DocType != "" && !strings.HasPrefix(strings.ToLower(label), strings.ToLower(r.DocType)) {
			label = r.DocType + " " + label
		}
		s := fmt.Sprintf("[%s](%s) — %s", mdEscape(label), vbplDocURL(id), mdEscape(clipRunes(r.Title, catalogueMaxTitleRunes)))
		if r.Status != "" {
			s += " _(" + mdEscape(r.Status) + ")_"
		}
		return s
	}
	return fmt.Sprintf("[Văn bản %s](%s)", id, vbplDocURL(id))
}

func sortDocsNewestFirst(list []*catalogueDoc) {
	sort.SliceStable(list, func(i, j int) bool {
		a, b := list[i].Doc, list[j].Doc
		ta, tb := dateKey(a.IssuedOn), dateKey(b.IssuedOn)
		if ta != tb {
			return ta > tb
		}
		return a.DocumentNumber < b.DocumentNumber
	})
}

func dateKey(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.Format("2006-01-02")
}

// catalogueMaxFolderRunes keeps folder names inside wiki_folders.name
// (varchar 255) with room for multi-byte Vietnamese letters.
const catalogueMaxFolderRunes = 120

// splitFields splits vbpl's comma-joined lĩnh vực string. vbpl joins a
// document's fields with ", " and writes each field name with a capital
// first letter, while a comma inside one name is followed by a lowercase
// continuation ("Thuế, phí và lệ phí", "Cán bộ, công chức"). So ", " starts
// a new field only when the next fragment begins with an uppercase letter
// or a digit.
func splitFields(field string) []string {
	field = strings.TrimSpace(field)
	if field == "" {
		return []string{catalogueNoField}
	}
	var out []string
	seen := map[string]bool{}
	cur := ""
	for i, frag := range strings.Split(field, ", ") {
		frag = strings.TrimSpace(frag)
		if frag == "" {
			continue
		}
		r, _ := utf8.DecodeRuneInString(frag)
		if i == 0 || cur == "" || !(unicode.IsUpper(r) || unicode.IsDigit(r)) {
			if cur == "" {
				cur = frag
			} else {
				cur += ", " + frag
			}
			continue
		}
		if !seen[cur] {
			seen[cur] = true
			out = append(out, cur)
		}
		cur = frag
	}
	if cur != "" && !seen[cur] {
		out = append(out, cur)
	}
	if len(out) == 0 {
		return []string{field}
	}
	return out
}

func docFields(d *catalogueDoc) []string {
	if len(d.Fields) > 0 {
		return d.Fields
	}
	return []string{catalogueNoField}
}

func primaryField(d *catalogueDoc) string {
	return docFields(d)[0]
}

func folderName(name string) string {
	return clipRunes(name, catalogueMaxFolderRunes)
}

func issuerName(doc *types.LegalDocument) string {
	if f := strings.TrimSpace(doc.Issuer); f != "" {
		return f
	}
	return catalogueNoIssuer
}

// renderCataloguePages builds every page of the KB: documents, lĩnh vực and
// cơ quan index pages, the two hubs, and the root index.
func renderCataloguePages(kb *types.KnowledgeBase, docs []*catalogueDoc, rows map[string]*types.LegalDocument) []*cataloguePage {
	byID := map[string]*catalogueDoc{}
	for _, d := range docs {
		byID[d.Doc.ID] = d
	}
	pages := make([]*cataloguePage, 0, len(docs)+64)

	byField := map[string][]*catalogueDoc{}
	byIssuer := map[string][]*catalogueDoc{}
	byStatus := map[string]int{}
	for _, d := range docs {
		for _, f := range docFields(d) {
			byField[f] = append(byField[f], d)
		}
		byIssuer[issuerName(d.Doc)] = append(byIssuer[issuerName(d.Doc)], d)
		byStatus[statusName(d.Doc)]++
	}

	// Document pages.
	for _, d := range docs {
		pages = append(pages, renderDocPage(d, byID, rows))
	}

	// Lĩnh vực index pages.
	fields := sortedKeys(byField)
	for _, f := range fields {
		list := byField[f]
		sortDocsNewestFirst(list)
		pages = append(pages, renderGroupIndex("Lĩnh vực", f, catalogueFieldSlug(f), catalogueFieldHub, "Tất cả lĩnh vực", list))
	}
	// Cơ quan ban hành index pages.
	issuers := sortedKeys(byIssuer)
	for _, is := range issuers {
		list := byIssuer[is]
		sortDocsNewestFirst(list)
		pages = append(pages, renderGroupIndex("Cơ quan ban hành", is, catalogueIssuerSlug(is), catalogueIssuerHub, "Tất cả cơ quan ban hành", list))
	}

	// Hubs.
	pages = append(pages, renderHub("Lĩnh vực", catalogueFieldHub, fields, byField, catalogueFieldSlug))
	pages = append(pages, renderHub("Cơ quan ban hành", catalogueIssuerHub, issuers, byIssuer, catalogueIssuerSlug))

	// Root.
	pages = append(pages, renderRoot(kb, docs, fields, byField, byStatus))
	return pages
}

func statusName(doc *types.LegalDocument) string {
	if s := strings.TrimSpace(doc.Status); s != "" {
		return s
	}
	return "Chưa xác định"
}

func sortedKeys[T any](m map[string]T) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func renderDocPage(d *catalogueDoc, byID map[string]*catalogueDoc, rows map[string]*types.LegalDocument) *cataloguePage {
	doc := d.Doc
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n", mdEscape(docHeading(doc)))
	if t := strings.TrimSpace(doc.Title); t != "" {
		fmt.Fprintf(&b, "**%s**\n\n", mdEscape(t))
	}

	b.WriteString("| Thuộc tính | Giá trị |\n|---|---|\n")
	row := func(k, v string) {
		if strings.TrimSpace(v) != "" {
			fmt.Fprintf(&b, "| %s | %s |\n", k, v)
		}
	}
	row("Loại văn bản", mdEscape(doc.DocType))
	row("Số hiệu", mdEscape(doc.DocumentNumber))
	row("Cơ quan ban hành", fmt.Sprintf("[[%s|%s]]", catalogueIssuerSlug(issuerName(doc)), mdEscape(issuerName(doc))))
	signer := strings.TrimSpace(doc.Signer)
	if st := strings.TrimSpace(doc.SignerTitle); st != "" && signer != "" {
		signer += " (" + st + ")"
	}
	row("Người ký", mdEscape(signer))
	row("Ngày ban hành", vnDate(doc.IssuedOn))
	row("Ngày có hiệu lực", vnDate(doc.EffectiveOn))
	row("Ngày hết hiệu lực", vnDate(doc.ExpiredOn))
	row("Tình trạng hiệu lực", mdEscape(statusName(doc)))
	fieldLinks := make([]string, 0, len(docFields(d)))
	for _, f := range docFields(d) {
		fieldLinks = append(fieldLinks, fmt.Sprintf("[[%s|%s]]", catalogueFieldSlug(f), mdEscape(f)))
	}
	row("Lĩnh vực", strings.Join(fieldLinks, ", "))
	row("Nguồn", fmt.Sprintf("[vbpl.vn](%s)", vbplDocURL(doc.ID)))
	b.WriteString("\n")

	if s := strings.TrimSpace(d.Summary); s != "" {
		b.WriteString("## Tóm tắt\n\n")
		b.WriteString(s)
		b.WriteString("\n\n")
	}

	var rel strings.Builder
	writeRelations := func(groups map[types.LegalRelation][]string, labels map[types.LegalRelation]string) {
		for _, r := range catalogueRelationOrder {
			ids := groups[r]
			if len(ids) == 0 {
				continue
			}
			ids = orderRelatedIDs(ids, byID, rows)
			fmt.Fprintf(&rel, "### %s (%d)\n\n", labels[r], len(ids))
			limit := catalogueRelationCap(r)
			for i, id := range ids {
				if i >= limit {
					fmt.Fprintf(&rel, "- … và %d văn bản khác (xem trên [vbpl.vn](%s))\n", len(ids)-limit, vbplDocURL(doc.ID))
					break
				}
				fmt.Fprintf(&rel, "- %s\n", docLink(id, byID, rows))
			}
			rel.WriteString("\n")
		}
	}
	writeRelations(d.Outgoing, catalogueOutgoingLabel)
	writeRelations(d.Incoming, catalogueIncomingLabel)
	if rel.Len() > 0 {
		b.WriteString("## Quan hệ với văn bản khác\n\n")
		b.WriteString(rel.String())
	}

	fmt.Fprintf(&b, "---\n[[%s|Lĩnh vực: %s]] · [[%s|%s]] · [[%s|Trang chủ]]\n",
		catalogueFieldSlug(primaryField(d)), mdEscape(primaryField(d)),
		catalogueIssuerSlug(issuerName(doc)), mdEscape(issuerName(doc)),
		catalogueRootSlug)

	title := docHeading(doc)
	if t := strings.TrimSpace(doc.Title); t != "" {
		title = clipRunes(title+" – "+t, 500)
	}
	summary := firstLine(d.Summary)
	if summary == "" {
		summary = clipRunes(doc.Title, 300)
	}
	aliases := []string{}
	return &cataloguePage{
		Slug:       d.Slug,
		Title:      title,
		PageType:   types.WikiPageTypeSummary,
		Summary:    summary,
		Content:    b.String(),
		FolderPath: []string{"Lĩnh vực", folderName(primaryField(d))},
		SourceRefs: []string{d.Knowledge.ID},
		Meta: map[string]any{
			"document_id":     doc.ID,
			"document_number": doc.DocumentNumber,
			"doc_type":        doc.DocType,
			"status":          statusName(doc),
			"fields":          docFields(d),
			"issuer":          issuerName(doc),
			"aliases":         aliases,
		},
	}
}

// orderRelatedIDs lists documents present in this KB first (they get
// in-wiki links), then the rest, each group newest first.
func orderRelatedIDs(ids []string, byID map[string]*catalogueDoc, rows map[string]*types.LegalDocument) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	rank := func(id string) (int, string, string) {
		if d := byID[id]; d != nil {
			return 0, dateKey(d.Doc.IssuedOn), d.Doc.DocumentNumber
		}
		if r := rows[id]; r != nil {
			return 1, dateKey(r.IssuedOn), r.DocumentNumber
		}
		return 2, "", id
	}
	sort.SliceStable(out, func(i, j int) bool {
		gi, di, ni := rank(out[i])
		gj, dj, nj := rank(out[j])
		if gi != gj {
			return gi < gj
		}
		if di != dj {
			return di > dj
		}
		return ni < nj
	})
	return out
}

func firstLine(s string) string {
	for _, line := range strings.Split(strings.TrimSpace(s), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			return clipRunes(line, 300)
		}
	}
	return ""
}

func renderGroupIndex(kind, name, slug, hubSlug, hubLabel string, list []*catalogueDoc) *cataloguePage {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s: %s\n\n", kind, mdEscape(name))
	fmt.Fprintf(&b, "%d văn bản trong kho này. [[%s|%s]] · [[%s|Trang chủ]]\n\n", len(list), hubSlug, hubLabel, catalogueRootSlug)

	groups := map[string][]*catalogueDoc{}
	for _, d := range list {
		groups[statusName(d.Doc)] = append(groups[statusName(d.Doc)], d)
	}
	order := append([]string{}, catalogueStatusOrder...)
	for _, k := range sortedKeys(groups) {
		found := false
		for _, o := range order {
			if o == k {
				found = true
				break
			}
		}
		if !found {
			order = append(order, k)
		}
	}
	written := 0
	for _, st := range order {
		g := groups[st]
		if len(g) == 0 {
			continue
		}
		fmt.Fprintf(&b, "## %s (%d)\n\n", mdEscape(st), len(g))
		for _, d := range g {
			if written >= catalogueMaxIndexEntries {
				fmt.Fprintf(&b, "- … danh sách được cắt ở %d văn bản\n", catalogueMaxIndexEntries)
				break
			}
			fmt.Fprintf(&b, "- [[%s|%s]] — %s", d.Slug, mdEscape(docDisplayNumber(d.Doc)), mdEscape(clipRunes(d.Doc.Title, catalogueMaxTitleRunes)))
			if dt := vnDate(d.Doc.IssuedOn); dt != "" {
				fmt.Fprintf(&b, " _(ban hành %s)_", dt)
			}
			b.WriteString("\n")
			written++
		}
		b.WriteString("\n")
	}
	return &cataloguePage{
		Slug:     slug,
		Title:    kind + ": " + name,
		PageType: types.WikiPageTypeIndex,
		Summary:  fmt.Sprintf("%d văn bản thuộc %s %s", len(list), strings.ToLower(kind), name),
		Content:  b.String(),
		Meta:     map[string]any{"group_kind": kind, "group_name": name, "count": len(list)},
	}
}

func renderHub(kind, slug string, names []string, groups map[string][]*catalogueDoc, slugOf func(string) string) *cataloguePage {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n", kind)
	fmt.Fprintf(&b, "%d nhóm. [[%s|Trang chủ]]\n\n", len(names), catalogueRootSlug)
	for _, n := range names {
		fmt.Fprintf(&b, "- [[%s|%s]] (%d)\n", slugOf(n), mdEscape(n), len(groups[n]))
	}
	return &cataloguePage{
		Slug:     slug,
		Title:    kind,
		PageType: types.WikiPageTypeIndex,
		Summary:  fmt.Sprintf("Danh sách %s (%d nhóm)", strings.ToLower(kind), len(names)),
		Content:  b.String(),
		Meta:     map[string]any{"group_kind": kind, "count": len(names)},
	}
}

func renderRoot(kb *types.KnowledgeBase, docs []*catalogueDoc, fields []string, byField map[string][]*catalogueDoc, byStatus map[string]int) *cataloguePage {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n", mdEscape(kb.Name))
	fmt.Fprintf(&b, "Trang tổng quan được sinh tự động từ catalogue văn bản (không dùng mô hình ngôn ngữ). %d văn bản.\n\n", len(docs))

	b.WriteString("## Theo tình trạng hiệu lực\n\n")
	order := append([]string{}, catalogueStatusOrder...)
	for _, k := range sortedKeys(byStatus) {
		found := false
		for _, o := range order {
			if o == k {
				found = true
				break
			}
		}
		if !found {
			order = append(order, k)
		}
	}
	for _, st := range order {
		if n := byStatus[st]; n > 0 {
			fmt.Fprintf(&b, "- %s: %d\n", mdEscape(st), n)
		}
	}
	b.WriteString("\n## Duyệt theo\n\n")
	fmt.Fprintf(&b, "- [[%s|Lĩnh vực]] (%d)\n", catalogueFieldHub, len(fields))
	fmt.Fprintf(&b, "- [[%s|Cơ quan ban hành]]\n\n", catalogueIssuerHub)

	top := append([]string{}, fields...)
	sort.SliceStable(top, func(i, j int) bool {
		if len(byField[top[i]]) != len(byField[top[j]]) {
			return len(byField[top[i]]) > len(byField[top[j]])
		}
		return top[i] < top[j]
	})
	if len(top) > catalogueRootTopFields {
		top = top[:catalogueRootTopFields]
	}
	fmt.Fprintf(&b, "## Lĩnh vực nhiều văn bản nhất\n\n")
	for _, f := range top {
		fmt.Fprintf(&b, "- [[%s|%s]] (%d)\n", catalogueFieldSlug(f), mdEscape(f), len(byField[f]))
	}

	// Newest documents.
	newest := append([]*catalogueDoc{}, docs...)
	sortDocsNewestFirst(newest)
	if len(newest) > 20 {
		newest = newest[:20]
	}
	b.WriteString("\n## Văn bản mới nhất\n\n")
	for _, d := range newest {
		fmt.Fprintf(&b, "- [[%s|%s]] — %s", d.Slug, mdEscape(docDisplayNumber(d.Doc)), mdEscape(clipRunes(d.Doc.Title, catalogueMaxTitleRunes)))
		if dt := vnDate(d.Doc.IssuedOn); dt != "" {
			fmt.Fprintf(&b, " _(ban hành %s)_", dt)
		}
		b.WriteString("\n")
	}
	return &cataloguePage{
		Slug:     catalogueRootSlug,
		Title:    "Index",
		PageType: types.WikiPageTypeIndex,
		Summary:  fmt.Sprintf("Tổng quan %d văn bản của %s", len(docs), kb.Name),
		Content:  b.String(),
		Meta:     map[string]any{"count": len(docs)},
	}
}
