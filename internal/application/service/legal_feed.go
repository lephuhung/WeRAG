package service

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	stderrors "errors"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/Tencent/WeKnora/internal/vbpl"
)

// LegalFeedService adds documents from vbpl.vn to the public legal
// knowledge bases. The MCP tool legal_ingest_document and the SuperAdmin
// legal request queue both ingest through IngestFromPortal, so a document
// fed either way gets the same file, metadata and log row (and matches
// scripts/legal_catalog/ingest_vbpl.py byte for byte).
type LegalFeedService struct {
	db        *gorm.DB
	knowledge interfaces.KnowledgeService
	requests  interfaces.LegalRequestRepository
	// HTTP fetches from vbpl.vn; nil uses a default client (tests swap it).
	HTTP *http.Client
}

// NewLegalFeedService creates the feed service.
func NewLegalFeedService(db *gorm.DB, knowledge interfaces.KnowledgeService,
	requests interfaces.LegalRequestRepository,
) *LegalFeedService {
	return &LegalFeedService{db: db, knowledge: knowledge, requests: requests}
}

// LegalExpiredStatusPrefix starts the catalogue / portal status of a
// document that no longer applies at all ("Hết hiệu lực toàn bộ").
const LegalExpiredStatusPrefix = "Hết hiệu lực toàn bộ"

// LegalFeedActor is who made a feed write: an MCP endpoint or a SuperAdmin.
type LegalFeedActor struct {
	EndpointID string
	TenantID   uint64
	UserID     string
	// Channel is the knowledge channel of created documents ("mcp", "web").
	Channel string
}

// LegalFeedEntry is one row of legal_feed_log.
type LegalFeedEntry struct {
	Action          string
	VBPLID          string
	KnowledgeID     string
	KnowledgeBaseID string
	DocumentNumber  string
	Reason          string
	Detail          map[string]any
}

// LegalIngestResult reports one ingest.
type LegalIngestResult struct {
	// Result is "created", "replaced" or "unchanged".
	Result              string
	VBPLID              string
	KnowledgeID         string
	KnowledgeBaseID     string
	ReplacedKnowledgeID string
	DocumentNumber      string
	DocType             string
	Status              string
	VBPLMarkup          bool
	// Knowledge is the created document (nil when unchanged).
	Knowledge *types.Knowledge
}

// LegalFeedRefusal is an ingest the feed will not do (fully expired, no
// full text, held by another KB, a bad id): the caller's input, not a
// failure — an HTTP API answers it with 4xx.
type LegalFeedRefusal struct{ msg string }

func (e *LegalFeedRefusal) Error() string { return e.msg }

func refuse(format string, args ...any) error {
	return &LegalFeedRefusal{msg: fmt.Sprintf(format, args...)}
}

// IngestFromPortal fetches one document from vbpl.vn and adds it to kb, a
// public KB (no owner organization). ctx must already carry the write
// grant for kb (the caller authorizes: endpoint scope or SuperAdmin).
// Idempotent: the same bytes in kb are "unchanged", other bytes replace
// the old copy, a copy in another public KB is refused. On success the
// write is logged and open requests for the document are marked added.
func (s *LegalFeedService) IngestFromPortal(
	ctx context.Context, vbplID string, kb *types.KnowledgeBase, actor LegalFeedActor, reason string,
) (*LegalIngestResult, error) {
	if s == nil || s.knowledge == nil || s.db == nil {
		return nil, fmt.Errorf("knowledge service is not available")
	}
	vbplID = strings.TrimSpace(vbplID)
	if vbplID == "" || kb == nil {
		return nil, refuse("vbpl_id and knowledge_base_id are required")
	}
	if kb.OwnerTenantID != 0 {
		return nil, refuse("knowledge base %q is not a public legal knowledge base", kb.ID)
	}
	detail, err := vbpl.FetchDetail(ctx, s.HTTP, vbplID)
	if err != nil {
		var missing *vbpl.NoDocumentError
		if stderrors.As(err, &missing) {
			return nil, refuse("vbpl.vn không có văn bản với id %s (danh mục có thể đã cũ)", missing.ID)
		}
		if strings.Contains(err.Error(), "is not a vbpl.vn document id") {
			return nil, &LegalFeedRefusal{msg: err.Error()}
		}
		return nil, err
	}
	status := strings.TrimSpace(detail.EffStatus.Name)
	if strings.HasPrefix(status, LegalExpiredStatusPrefix) {
		return nil, refuse("document %s (%s) is \"%s\": fully expired documents keep their catalogue "+
			"row only and are never fed into a knowledge base", vbplID, detail.DocNum, status)
	}
	content := []byte(detail.Content)
	if len(content) < vbpl.MinHTMLBytes {
		return nil, refuse("document %s has no published full text on vbpl.vn (%d bytes)", vbplID, len(content))
	}
	sum := md5.Sum(content)
	hash := hex.EncodeToString(sum[:])
	res := &LegalIngestResult{
		VBPLID: vbplID, KnowledgeBaseID: kb.ID, DocumentNumber: detail.DocNum,
		DocType: detail.DocType.Name, Status: status, VBPLMarkup: vbpl.LooksLikeVBPL(content),
	}

	// Where is this document already? Same KB + same bytes → nothing to do;
	// same KB + other bytes → replace; another public KB → refuse (one copy).
	var existing []struct {
		ID              string
		KnowledgeBaseID string
		FileHash        string
	}
	if err := s.db.WithContext(ctx).Raw(`SELECT k.id, k.knowledge_base_id, k.file_hash FROM knowledges k
		JOIN knowledge_bases kb ON kb.id = k.knowledge_base_id AND kb.owner_tenant_id = 0 AND kb.deleted_at IS NULL
		WHERE k.deleted_at IS NULL AND k.metadata->>'external_id' = ?`, vbplID).Scan(&existing).Error; err != nil {
		return nil, fmt.Errorf("lookup failed: %w", err)
	}
	for _, e := range existing {
		if e.KnowledgeBaseID != kb.ID {
			return nil, refuse("document %s is already held by knowledge base %s as %s; a document lives "+
				"in one knowledge base only", vbplID, e.KnowledgeBaseID, e.ID)
		}
		if e.FileHash == hash {
			res.Result, res.KnowledgeID = "unchanged", e.ID
			s.markRequestsAdded(ctx, res, actor)
			return res, nil
		}
		if err := s.knowledge.DeleteKnowledge(ctx, e.ID); err != nil {
			return nil, fmt.Errorf("failed to delete the outdated copy %s: %w", e.ID, err)
		}
		res.ReplacedKnowledgeID = e.ID
	}

	name := vbpl.FileName(detail)
	fh, err := memoryFileHeader("file", name, content)
	if err != nil {
		return nil, fmt.Errorf("failed to stage the file: %w", err)
	}
	channel := actor.Channel
	if channel == "" {
		channel = types.ChannelAPI
	}
	created, err := s.knowledge.CreateKnowledgeFromFile(ctx, kb.ID, fh, vbpl.Metadata(detail), nil, name, nil, channel, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to add document: %w", err)
	}
	res.Result = "created"
	if res.ReplacedKnowledgeID != "" {
		res.Result = "replaced"
	}
	res.KnowledgeID, res.Knowledge = created.ID, created
	logDetail := map[string]any{"replaced_knowledge_id": res.ReplacedKnowledgeID, "status": status, "md5": hash,
		"vbpl_markup": res.VBPLMarkup}
	if actor.UserID != "" {
		logDetail["actor_user_id"] = actor.UserID
	}
	WriteLegalFeedLog(ctx, s.db, actor, LegalFeedEntry{
		Action: "ingest_" + res.Result, VBPLID: vbplID, KnowledgeID: created.ID, KnowledgeBaseID: kb.ID,
		DocumentNumber: detail.DocNum, Reason: reason, Detail: logDetail,
	})
	s.markRequestsAdded(ctx, res, actor)
	return res, nil
}

// markRequestsAdded closes the open requests for the ingested document.
func (s *LegalFeedService) markRequestsAdded(ctx context.Context, res *LegalIngestResult, actor LegalFeedActor) {
	if s.requests == nil {
		return
	}
	by := actor.UserID
	if by == "" {
		by = actor.EndpointID
	}
	if n, err := s.requests.MarkAddedByDocument(ctx, res.VBPLID, res.KnowledgeID, res.KnowledgeBaseID, by); err != nil {
		logger.Warnf(ctx, "[LegalFeed] marking requests of %s added: %v", res.VBPLID, err)
	} else if n > 0 {
		logger.Infof(ctx, "[LegalFeed] %d request(s) for %s marked added", n, res.VBPLID)
	}
}

// WriteLegalFeedLog appends one row to legal_feed_log; a failure is
// logged, never returned, so the write that succeeded is reported as such.
func WriteLegalFeedLog(ctx context.Context, db *gorm.DB, actor LegalFeedActor, e LegalFeedEntry) {
	if db == nil {
		return
	}
	var detail any
	if e.Detail != nil {
		if b, err := json.Marshal(e.Detail); err == nil && len(b) > 0 {
			detail = string(b)
		}
	}
	err := db.WithContext(ctx).Exec(`INSERT INTO legal_feed_log
		(action, vbpl_id, knowledge_id, knowledge_base_id, document_number, reason, actor_endpoint_id, actor_tenant_id, detail, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		e.Action, e.VBPLID, e.KnowledgeID, e.KnowledgeBaseID, e.DocumentNumber, e.Reason, actor.EndpointID,
		actor.TenantID, detail, time.Now().UTC()).Error
	if err != nil {
		logger.Warnf(ctx, "[LegalFeed] legal_feed_log insert failed (%s %s): %v", e.Action, e.VBPLID, err)
	}
}

// memoryFileHeader builds a *multipart.FileHeader from bytes, the type
// KnowledgeService.CreateKnowledgeFromFile expects from an HTTP upload.
func memoryFileHeader(field, name string, content []byte) (*multipart.FileHeader, error) {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	hdr := textproto.MIMEHeader{}
	hdr.Set("Content-Disposition", fmt.Sprintf(`form-data; name="%s"; filename="%s"`, field, strings.ReplaceAll(name, `"`, "'")))
	hdr.Set("Content-Type", "text/html")
	part, err := w.CreatePart(hdr)
	if err != nil {
		return nil, err
	}
	if _, err := part.Write(content); err != nil {
		return nil, err
	}
	if err := w.Close(); err != nil {
		return nil, err
	}
	form, err := multipart.NewReader(&buf, w.Boundary()).ReadForm(int64(len(content)) + 1<<20)
	if err != nil {
		return nil, err
	}
	files := form.File[field]
	if len(files) == 0 {
		return nil, fmt.Errorf("multipart form lost the file part")
	}
	return files[0], nil
}

// MemoryFileHeader is memoryFileHeader for other packages (the MCP tests).
func MemoryFileHeader(field, name string, content []byte) (*multipart.FileHeader, error) {
	return memoryFileHeader(field, name, content)
}
