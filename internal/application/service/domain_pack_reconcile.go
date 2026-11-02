package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/Tencent/WeKnora/internal/agent/domainpacks"
	apperrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/Tencent/WeKnora/internal/vietnamese_legal"
)

// domainPackReconcileLookup caps the catalogue rows read per number; more
// than a handful sharing one số hiệu is already "ambiguous".
const domainPackReconcileLookup = 20

// domainPackNoteRelations are the incoming relations a status note reports.
var domainPackNoteRelations = []types.LegalRelation{
	types.LegalRelationReplaces, types.LegalRelationExpires, types.LegalRelationPartiallyExpires,
	types.LegalRelationSuspends, types.LegalRelationAmends,
}

// DomainPackReconciler checks a domain pack's catalogue.yaml against the
// legal catalogue (docs/domain-pack-standard.md §4 layer b, §5 step 7). It
// reads legal_documents and never changes it. Like the chat's legal request
// gate (legalMissingDocumentGate) it records a legal document request for a
// document no public knowledge base holds: with the catalogue id when the
// catalogue knows the number (the queue's ingest needs it), without when it
// does not.
type DomainPackReconciler struct {
	packs    *domainpacks.Registry
	catalog  interfaces.LegalCatalogRepository
	requests interfaces.LegalRequestRepository
	reviews  interfaces.LegalReviewRepository
	agents   agentLookup
	// edits of the pack formulas (domain_pack_formula_edit.go)
	overrides interfaces.DomainPackFormulaOverrideRepository
	users     interfaces.UserRepository
	now       func() time.Time
}

// NewDomainPackReconciler creates the reconciler.
func NewDomainPackReconciler(packs *domainpacks.Registry, catalog interfaces.LegalCatalogRepository,
	requests interfaces.LegalRequestRepository, reviews interfaces.LegalReviewRepository,
	agents interfaces.DomainPackAgentRepository,
	overrides interfaces.DomainPackFormulaOverrideRepository, users interfaces.UserRepository,
) *DomainPackReconciler {
	r := &DomainPackReconciler{packs: packs, catalog: catalog, requests: requests, reviews: reviews,
		overrides: overrides, users: users, now: domainPackNow}
	if agents != nil {
		r.agents = agents
	}
	return r
}

// reconcileRun is one Reconcile call: the pack and who asked.
type reconcileRun struct {
	pack   *domainpacks.Pack
	caller types.Caller
	now    time.Time
}

// Reconcile looks every catalogue number of pack name up by number_key and
// sorts it into found, missing or ambiguous. A number whose check fails is
// listed in Errors and the others are still reported. The caller (from ctx)
// is stored on the requests this call records.
func (r *DomainPackReconciler) Reconcile(ctx context.Context, name string) (*types.DomainPackReconcileResult, error) {
	if r == nil || r.packs == nil || r.catalog == nil || r.requests == nil || r.reviews == nil {
		return nil, apperrors.NewInternalServerError("domain pack reconcile is not available")
	}
	pack, ok := r.packs.Get(strings.TrimSpace(name))
	if !ok {
		return nil, apperrors.NewNotFoundError(fmt.Sprintf("domain pack %q is not in this build", name))
	}
	out := &types.DomainPackReconcileResult{
		Pack:      pack.Name(),
		Version:   pack.Manifest.Metadata.Version,
		Found:     []types.DomainPackReconcileFound{},
		Missing:   []types.DomainPackReconcileMissing{},
		Ambiguous: []types.DomainPackReconcileAmbiguous{},
		Errors:    []types.DomainPackReconcileError{},
	}
	catalogue := pack.Catalogue()
	if catalogue == nil {
		return out, nil
	}
	run := reconcileRun{pack: pack, caller: types.CallerFromContext(ctx), now: r.now()}
	for _, entry := range catalogue.Documents {
		ref := types.DomainPackCatalogueRef{
			Number: strings.TrimSpace(entry.Number), Title: entry.Title, UsedFor: entry.UsedFor,
		}
		if err := r.reconcileOne(ctx, run, ref, out); err != nil {
			logger.Warnf(ctx, "Domain pack %s reconcile of %s: %v", pack.Name(), ref.Number, err)
			out.Errors = append(out.Errors, types.DomainPackReconcileError{Number: ref.Number, Error: err.Error()})
		}
	}
	logger.Infof(ctx, "Domain pack %s reconciled: %d found, %d missing, %d ambiguous, %d failed",
		out.Pack, len(out.Found), len(out.Missing), len(out.Ambiguous), len(out.Errors))
	return out, nil
}

func (r *DomainPackReconciler) reconcileOne(ctx context.Context, run reconcileRun,
	ref types.DomainPackCatalogueRef, out *types.DomainPackReconcileResult,
) error {
	lk, err := r.lookup(ctx, run.now, ref)
	if err != nil {
		return err
	}
	if len(lk.rows) == 0 {
		req, err := r.request(ctx, run, ref, &types.LegalDocumentRequest{NumberKey: lk.key, DocumentNumber: ref.Number, Title: ref.Title})
		if err != nil {
			return err
		}
		out.Missing = append(out.Missing, types.DomainPackReconcileMissing{DomainPackCatalogueRef: ref, Request: req})
		return nil
	}
	// no KB holds any of them: ask for the text, as the gate does — of the
	// rows not fully expired when there are live ones beside expired ones,
	// and not at all when every row is fully expired (the gate shows the
	// "expired" card instead; here the status_note tells the SuperAdmin to
	// fix the pack)
	if !lk.held() && !allFullyExpired(lk.rows) {
		for i, d := range lk.rows {
			if len(lk.rows) > 1 && expiredBesideLive(lk.rows, d) {
				continue
			}
			req, err := r.request(ctx, run, ref, &types.LegalDocumentRequest{
				NumberKey: lk.key, DocumentNumber: firstNonEmpty(strings.TrimSpace(d.DocumentNumber), ref.Number),
				Title: d.Title, DocType: d.DocType,
				LegalDocumentID: d.ID, Source: types.LegalRequestSourceCatalogue,
			})
			if err != nil {
				return err
			}
			lk.docs[i].Request = req
		}
	}
	if len(lk.docs) == 1 {
		out.Found = append(out.Found, types.DomainPackReconcileFound{DomainPackCatalogueRef: ref, Document: lk.docs[0]})
		return nil
	}
	out.Ambiguous = append(out.Ambiguous, types.DomainPackReconcileAmbiguous{DomainPackCatalogueRef: ref, Documents: lk.docs})
	return nil
}

// numberLookup is the read-only inspection of one catalogue number: the
// catalogue rows holding it exactly, projected with their status notes and
// whether a public KB holds the text. It writes nothing.
type numberLookup struct {
	key  string
	rows []*types.LegalDocument
	docs []types.DomainPackReconcileDocument
}

func (lk *numberLookup) held() bool {
	for _, d := range lk.docs {
		if d.HeldInKB {
			return true
		}
	}
	return false
}

func (r *DomainPackReconciler) lookup(ctx context.Context, now time.Time, ref types.DomainPackCatalogueRef) (*numberLookup, error) {
	lk := &numberLookup{key: vietnamese_legal.CatalogNumberKey(ref.Number)}
	docs, err := r.catalog.FindByNumberKey(ctx, lk.key, domainPackReconcileLookup)
	if err != nil {
		return nil, fmt.Errorf("catalogue lookup: %w", err)
	}
	// FindByNumberKey falls back to a prefix match; a pack names full
	// numbers, so only the exact key counts
	for _, d := range docs {
		if d != nil && d.NumberKey == lk.key {
			lk.rows = append(lk.rows, d)
		}
	}
	for _, d := range lk.rows {
		doc, err := r.document(ctx, d, now)
		if err != nil {
			return nil, err
		}
		lk.docs = append(lk.docs, doc)
	}
	if len(lk.docs) > 0 {
		if err := r.markHeld(ctx, lk.docs); err != nil {
			return nil, err
		}
	}
	return lk, nil
}

// allFullyExpired reports whether every row is "Hết hiệu lực toàn bộ".
func allFullyExpired(rows []*types.LegalDocument) bool {
	for _, d := range rows {
		if !strings.HasPrefix(strings.TrimSpace(d.Status), LegalExpiredStatusPrefix) {
			return false
		}
	}
	return len(rows) > 0
}

// markHeld sets HeldInKB like the chat gate decides a document is held: an
// approved legal identity link, or a public-KB copy carrying its catalogue
// id as external_id (ingested, not linked yet).
func (r *DomainPackReconciler) markHeld(ctx context.Context, docs []types.DomainPackReconcileDocument) error {
	ids := make([]string, 0, len(docs))
	for _, d := range docs {
		ids = append(ids, d.ID)
	}
	approved, err := r.reviews.ApprovedDocumentIDs(ctx, ids)
	if err != nil {
		return fmt.Errorf("approved links: %w", err)
	}
	ingested, err := r.requests.PublicKnowledgeByExternalIDs(ctx, ids)
	if err != nil {
		return fmt.Errorf("public copies: %w", err)
	}
	for i := range docs {
		docs[i].HeldInKB = approved[docs[i].ID] || ingested[docs[i].ID]
	}
	return nil
}

// expiredBesideLive reports whether d is fully expired while another row of
// the same number is not (legalDropExpiredBesideLive of the gate).
func expiredBesideLive(rows []*types.LegalDocument, d *types.LegalDocument) bool {
	if !strings.HasPrefix(strings.TrimSpace(d.Status), LegalExpiredStatusPrefix) {
		return false
	}
	for _, o := range rows {
		if !strings.HasPrefix(strings.TrimSpace(o.Status), LegalExpiredStatusPrefix) {
			return true
		}
	}
	return false
}

// request reports the request for (NumberKey, LegalDocumentID) of base: an
// existing one (any status — an open one is not bumped, a rejected or added
// one is not reopened) or a new one whose Detail names the pack.
func (r *DomainPackReconciler) request(ctx context.Context, run reconcileRun,
	ref types.DomainPackCatalogueRef, base *types.LegalDocumentRequest,
) (*types.DomainPackReconcileRequest, error) {
	existing, err := r.requests.ListByNumberKey(ctx, base.NumberKey)
	if err != nil {
		return nil, fmt.Errorf("legal requests: %w", err)
	}
	var same []*types.LegalDocumentRequest
	for _, row := range existing {
		if row != nil && row.LegalDocumentID == base.LegalDocumentID {
			same = append(same, row)
		}
	}
	if row := preferOpenRequest(same); row != nil {
		return &types.DomainPackReconcileRequest{ID: row.ID, Status: row.Status}, nil
	}
	detail, err := json.Marshal(map[string]any{
		types.DomainPackRequestDetailOrigin: types.DomainPackRequestOriginPrefix + run.pack.Name(),
		"pack_version":                      run.pack.Manifest.Metadata.Version,
		"used_for":                          ref.UsedFor,
	})
	if err != nil {
		return nil, err
	}
	row := *base
	row.LastTenantID = run.caller.TenantID
	row.LastUserID = run.caller.UserID
	row.LastQuery = "Danh mục bộ kỹ năng " + run.pack.Name() + ": " + ref.UsedFor
	row.Detail = types.JSON(detail)
	stored, err := r.requests.Record(ctx, &row)
	if err != nil {
		return nil, fmt.Errorf("recording a request: %w", err)
	}
	return &types.DomainPackReconcileRequest{ID: stored.ID, Status: stored.Status, Created: true}, nil
}

// preferOpenRequest picks the request to report: the first open one, else
// the most recent of any status, nil when there is none.
func preferOpenRequest(rows []*types.LegalDocumentRequest) *types.LegalDocumentRequest {
	var latest *types.LegalDocumentRequest
	for _, row := range rows {
		if row == nil {
			continue
		}
		if types.IsLegalRequestOpen(row.Status) {
			return row
		}
		latest = row
	}
	return latest
}

// document projects a catalogue row and notes what needs checking.
func (r *DomainPackReconciler) document(ctx context.Context, d *types.LegalDocument, now time.Time) (types.DomainPackReconcileDocument, error) {
	out := types.DomainPackReconcileDocument{
		ID: d.ID, DocumentNumber: d.DocumentNumber, Title: d.Title, DocType: d.DocType,
		Issuer: d.Issuer, Status: d.Status,
		EffectiveOn: reconcileDay(d.EffectiveOn), ExpiredOn: reconcileDay(d.ExpiredOn),
	}
	links, err := r.catalog.ListLinks(ctx, d.ID, nil, domainPackNoteRelations)
	if err != nil {
		return out, fmt.Errorf("catalogue links of %s: %w", d.DocumentNumber, err)
	}
	out.StatusNote = reconcileStatusNote(d, links, now)
	return out, nil
}

// reconcileStatusNote says why what the pack says about a catalogue row
// needs checking: its status (expired, partly expired, suspended, not yet
// in force), its dates, or a document that replaced, ended, suspended or
// amended it. "" when none of these holds.
func reconcileStatusNote(d *types.LegalDocument, links []types.LegalRelationLink, now time.Time) string {
	var notes []string
	status := strings.TrimSpace(d.Status)
	lower := strings.ToLower(status)
	switch {
	case strings.HasPrefix(status, LegalExpiredStatusPrefix):
		notes = append(notes, "Danh mục ghi \""+status+"\"")
	case lower == "hết hiệu lực một phần":
		notes = append(notes, "Danh mục ghi \"Hết hiệu lực một phần\": kiểm tra phần gói dựa vào")
	case strings.Contains(lower, "ngưng hiệu lực") || strings.HasPrefix(lower, "đình chỉ"):
		notes = append(notes, "Danh mục ghi \""+status+"\"")
	case lower == "chưa có hiệu lực":
		notes = append(notes, "Danh mục ghi \"Chưa có hiệu lực\"")
	}
	if d.EffectiveOn != nil && d.EffectiveOn.After(now) {
		notes = append(notes, "chưa có hiệu lực (hiệu lực từ "+d.EffectiveOn.Format("02/01/2006")+")")
	}
	if d.ExpiredOn != nil && !d.ExpiredOn.After(now) {
		notes = append(notes, "ngày hết hiệu lực "+d.ExpiredOn.Format("02/01/2006"))
	}
	for _, l := range links {
		if !l.Incoming || l.Other == nil {
			continue
		}
		other := strings.TrimSpace(l.Other.DocumentNumber)
		pending := l.Other.EffectiveOn != nil && l.Other.EffectiveOn.After(now)
		from := ""
		if l.Other.EffectiveOn != nil {
			from = " (hiệu lực " + l.Other.EffectiveOn.Format("02/01/2006") + ")"
		}
		switch l.Relation {
		case types.LegalRelationAmends:
			if pending {
				notes = append(notes, "sẽ được sửa đổi bởi "+other+from+", kiểm tra các điều gói trích")
			} else {
				notes = append(notes, "đã được sửa đổi bởi "+other+from+", kiểm tra các điều gói trích")
			}
		case types.LegalRelationReplaces, types.LegalRelationExpires, types.LegalRelationPartiallyExpires,
			types.LegalRelationSuspends:
			if pending {
				continue // the other document is not in force yet
			}
			verb := map[types.LegalRelation]string{
				types.LegalRelationReplaces:         "bị thay thế bởi ",
				types.LegalRelationExpires:          "bị làm hết hiệu lực bởi ",
				types.LegalRelationPartiallyExpires: "bị làm hết hiệu lực một phần bởi ",
				types.LegalRelationSuspends:         "bị ngưng hiệu lực bởi ",
			}[l.Relation]
			notes = append(notes, verb+other+from)
		}
	}
	return strings.Join(notes, "; ")
}

func reconcileDay(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.Format("2006-01-02")
}
