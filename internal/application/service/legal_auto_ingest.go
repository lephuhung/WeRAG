package service

import (
	"context"
	"encoding/json"
	stderrors "errors"
	"time"

	"github.com/Tencent/WeKnora/internal/application/access"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

const (
	// legalAutoIngestActor holds the lease and signs the worker's rows.
	legalAutoIngestActor = "system:auto-ingest"
	// A small batch every few minutes: a night's new documents are in by
	// morning and chat keeps the embedding and parsing capacity.
	legalAutoIngestTick  = 3 * time.Minute
	legalAutoIngestBatch = 5
	// legalAutoIngestScan bounds the rows read to find a batch: rows the
	// worker gave up on stay in the table.
	legalAutoIngestScan = 200
)

// LegalAutoIngest adds to the public KBs the documents the nightly vbpl.vn
// sync queued (types.LegalRequestAutoIngest). vbpl.vn is the standard, so no
// one reviews them: the ingest path approves their catalogue link itself.
// It runs without a caller (knowledges.created_by stays empty) on the KB
// write grant trusted workers get (access.WithKBTaskWrite).
type LegalAutoIngest struct {
	requests interfaces.LegalRequestRepository
	kbs      interfaces.KnowledgeBaseService
	tenants  interfaces.TenantRepository
	ingest   func(context.Context, string, *types.KnowledgeBase, LegalFeedActor, string) (*LegalIngestResult, error)
}

// NewLegalAutoIngest creates the worker.
func NewLegalAutoIngest(
	requests interfaces.LegalRequestRepository, kbs interfaces.KnowledgeBaseService,
	tenants interfaces.TenantRepository, feed *LegalFeedService,
) *LegalAutoIngest {
	return &LegalAutoIngest{requests: requests, kbs: kbs, tenants: tenants, ingest: feed.IngestFromPortal}
}

// StartLegalAutoIngest runs the worker until the returned func is called.
func StartLegalAutoIngest(w *LegalAutoIngest) (stop func()) {
	done := make(chan struct{})
	go func() {
		ticker := time.NewTicker(legalAutoIngestTick)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				w.Tick(context.Background())
			case <-done:
				return
			}
		}
	}()
	return func() { close(done) }
}

// Tick ingests one batch and returns how many documents it added.
func (w *LegalAutoIngest) Tick(ctx context.Context) (added int) {
	// A bare goroutine: a panic in the ingest path must not take the server down.
	defer func() {
		if r := recover(); r != nil {
			logger.Errorf(ctx, "[LegalAutoIngest] panic: %v", r)
		}
	}()
	rows, err := w.requests.NextAutoIngest(ctx, legalAutoIngestScan)
	if err != nil {
		logger.Warnf(ctx, "[LegalAutoIngest] listing: %v", err)
		return 0
	}
	if len(rows) == 0 {
		return 0
	}
	kbs, err := w.requests.PublicKnowledgeBases(ctx)
	if err != nil {
		logger.Warnf(ctx, "[LegalAutoIngest] public KBs: %v", err)
		return 0
	}
	targets := LegalTypeTargets(kbs)
	tried := 0
	for _, row := range rows {
		if tried == legalAutoIngestBatch {
			break
		}
		// A type without a KB waits here until one exists; a text update
		// goes to the KB holding the copy.
		if _, ok := targets[row.DocType]; (!ok && row.KnowledgeBaseID == "") || legalAutoIngestGivenUp(row) {
			continue
		}
		tried++
		if w.one(ctx, row, targets) {
			added++
		}
	}
	if tried > 0 {
		logger.Infof(ctx, "[LegalAutoIngest] added %d of %d tried; %d rows read", added, tried, len(rows))
	}
	return added
}

func legalAutoIngestGivenUp(row *types.LegalDocumentRequest) bool {
	var detail map[string]json.RawMessage
	if len(row.Detail) == 0 || json.Unmarshal(row.Detail, &detail) != nil {
		return false
	}
	v := detail[types.LegalRequestDetailAutoIngestSkip]
	return len(v) > 0 && string(v) != "null"
}

// one ingests a row. A document the portal has no HTML for after all goes to
// the operators; any other refusal (fully expired meanwhile, held by another
// KB, a stale id) closes the row with its reason; a transient failure is
// retried on a later tick.
func (w *LegalAutoIngest) one(ctx context.Context, row *types.LegalDocumentRequest, targets map[string]types.LegalPublicKB) bool {
	kbID := row.KnowledgeBaseID
	if kbID == "" {
		kbID = targets[row.DocType].ID
	}
	claimed, err := w.requests.Claim(ctx, row.ID, legalAutoIngestActor, true)
	if err != nil || claimed == nil {
		return false // another replica has it, or the row just closed
	}
	kb, err := w.kbs.GetKnowledgeBaseByIDOnly(ctx, kbID)
	if err != nil || kb == nil {
		w.retryLater(ctx, row, "knowledge base "+kbID+" is not available")
		return false
	}
	scoped, err := access.WithKBTaskWrite(ctx, kb, kb.TenantID)
	if err != nil {
		w.retryLater(ctx, row, "no write grant on knowledge base "+kb.ID)
		return false
	}
	if scoped, err = withKBWriteTenantInfo(scoped, kb, w.tenants); err != nil {
		w.retryLater(ctx, row, "owner of knowledge base "+kb.ID+": "+err.Error())
		return false
	}
	res, err := w.ingest(scoped, row.LegalDocumentID, kb, LegalFeedActor{TenantID: kb.TenantID, UserID: legalAutoIngestActor},
		"Tự nạp từ vbpl.vn ("+row.DocumentNumber+")")
	if err != nil {
		var refusal *LegalFeedRefusal
		if !stderrors.As(err, &refusal) {
			w.retryLater(ctx, row, err.Error())
			return false
		}
		// A refused text update keeps the copy the KB has: nothing for the
		// operators to fetch.
		if refusal.Portal == nil || row.KnowledgeBaseID != "" {
			logger.Warnf(ctx, "[LegalAutoIngest] %s (%s) closed: %s", row.LegalDocumentID, row.DocumentNumber, refusal.Error())
			if _, err := w.requests.Decide(ctx, row.ID, types.LegalRequestRejected, legalAutoIngestActor,
				"Tự nạp từ vbpl.vn: "+refusal.Error(), "", ""); err != nil {
				logger.Warnf(ctx, "[LegalAutoIngest] closing %s: %v", row.ID, err)
			}
			return false
		}
		// no HTML after all: the operators fetch it elsewhere
		w.giveUp(ctx, row, refusal.Error())
		if err := w.requests.ReturnToOperators(ctx, row.ID, types.LegalRequestSourceNoFullText); err != nil {
			logger.Warnf(ctx, "[LegalAutoIngest] returning %s: %v", row.ID, err)
		}
		return false
	}
	logger.Infof(ctx, "[LegalAutoIngest] %s (%s): %s as %s", row.LegalDocumentID, row.DocumentNumber, res.Result, res.KnowledgeID)
	return true
}

func (w *LegalAutoIngest) giveUp(ctx context.Context, row *types.LegalDocumentRequest, reason string) {
	logger.Warnf(ctx, "[LegalAutoIngest] %s (%s) not ingested: %s", row.LegalDocumentID, row.DocumentNumber, reason)
	w.note(ctx, row, types.LegalRequestDetailAutoIngestSkip, reason)
}

func (w *LegalAutoIngest) retryLater(ctx context.Context, row *types.LegalDocumentRequest, reason string) {
	logger.Warnf(ctx, "[LegalAutoIngest] %s (%s) will be retried: %s", row.LegalDocumentID, row.DocumentNumber, reason)
	w.note(ctx, row, types.LegalRequestDetailAutoIngestError, reason)
}

// note records why and releases the lease; touching the row also sends it to
// the back of the worker's queue.
func (w *LegalAutoIngest) note(ctx context.Context, row *types.LegalDocumentRequest, key, reason string) {
	if _, err := w.requests.UpdateDetail(ctx, row.ID, map[string]any{key: reason}); err != nil {
		logger.Warnf(ctx, "[LegalAutoIngest] noting %s: %v", row.ID, err)
	}
	if _, err := w.requests.Claim(ctx, row.ID, legalAutoIngestActor, false); err != nil {
		logger.Warnf(ctx, "[LegalAutoIngest] releasing %s: %v", row.ID, err)
	}
}
