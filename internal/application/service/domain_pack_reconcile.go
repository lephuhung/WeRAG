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

// DomainPackReconciler checks a domain pack's catalogue.yaml against the
// legal catalogue (docs/domain-pack-standard.md §4 layer b, §5 step 7). It
// reads legal_documents and never changes it; a number the catalogue lacks
// becomes a legal document request for the SuperAdmin queue, recorded the
// way the chat's legal request gate records one.
type DomainPackReconciler struct {
	packs    *domainpacks.Registry
	catalog  interfaces.LegalCatalogRepository
	requests interfaces.LegalRequestRepository
	now      func() time.Time
}

// NewDomainPackReconciler creates the reconciler.
func NewDomainPackReconciler(packs *domainpacks.Registry, catalog interfaces.LegalCatalogRepository,
	requests interfaces.LegalRequestRepository,
) *DomainPackReconciler {
	return &DomainPackReconciler{packs: packs, catalog: catalog, requests: requests, now: domainPackNow}
}

// Reconcile looks every catalogue number of pack name up by number_key and
// sorts it into found, missing or ambiguous. actorID is the SuperAdmin who
// asked; it is stored on a request this call records.
func (r *DomainPackReconciler) Reconcile(ctx context.Context, name, actorID string) (*types.DomainPackReconcileResult, error) {
	if r == nil || r.packs == nil || r.catalog == nil || r.requests == nil {
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
	}
	catalogue := pack.Catalogue()
	if catalogue == nil {
		return out, nil
	}
	now := r.now()
	for _, entry := range catalogue.Documents {
		ref := types.DomainPackCatalogueRef{
			Number: strings.TrimSpace(entry.Number), Title: entry.Title, UsedFor: entry.UsedFor,
		}
		key := vietnamese_legal.CatalogNumberKey(ref.Number)
		docs, err := r.catalog.FindByNumberKey(ctx, key, domainPackReconcileLookup)
		if err != nil {
			return nil, fmt.Errorf("catalogue lookup of %s: %w", ref.Number, err)
		}
		// FindByNumberKey falls back to a prefix match; a pack names full
		// numbers, so only the exact key counts
		exact := docs[:0]
		for _, d := range docs {
			if d != nil && d.NumberKey == key {
				exact = append(exact, d)
			}
		}
		switch len(exact) {
		case 0:
			missing, err := r.request(ctx, pack, ref, key, actorID)
			if err != nil {
				return nil, err
			}
			out.Missing = append(out.Missing, *missing)
		case 1:
			doc, err := r.document(ctx, exact[0], now)
			if err != nil {
				return nil, err
			}
			out.Found = append(out.Found, types.DomainPackReconcileFound{DomainPackCatalogueRef: ref, Document: doc})
		default:
			amb := types.DomainPackReconcileAmbiguous{DomainPackCatalogueRef: ref}
			for _, d := range exact {
				doc, err := r.document(ctx, d, now)
				if err != nil {
					return nil, err
				}
				amb.Documents = append(amb.Documents, doc)
			}
			out.Ambiguous = append(out.Ambiguous, amb)
		}
	}
	logger.Infof(ctx, "Domain pack %s reconciled: %d found, %d missing, %d ambiguous",
		out.Pack, len(out.Found), len(out.Missing), len(out.Ambiguous))
	return out, nil
}

// request reports the request for a number the catalogue lacks: an
// existing one (any status — an open one is not bumped, a rejected or
// added one is not reopened) or a new one marked pack:<name>.
func (r *DomainPackReconciler) request(ctx context.Context, pack *domainpacks.Pack,
	ref types.DomainPackCatalogueRef, key, actorID string,
) (*types.DomainPackReconcileMissing, error) {
	out := &types.DomainPackReconcileMissing{DomainPackCatalogueRef: ref}
	existing, err := r.requests.ListByNumberKey(ctx, key)
	if err != nil {
		return nil, fmt.Errorf("legal requests of %s: %w", ref.Number, err)
	}
	if row := preferOpenRequest(existing); row != nil {
		out.RequestID, out.RequestStatus = row.ID, row.Status
		return out, nil
	}
	origin := types.DomainPackRequestSourcePrefix + pack.Name()
	detail, err := json.Marshal(map[string]any{
		"origin":       origin,
		"pack_version": pack.Manifest.Metadata.Version,
		"used_for":     ref.UsedFor,
	})
	if err != nil {
		return nil, err
	}
	row, err := r.requests.Record(ctx, &types.LegalDocumentRequest{
		NumberKey:      key,
		DocumentNumber: ref.Number,
		Title:          ref.Title,
		Source:         origin,
		LastUserID:     actorID,
		LastQuery:      "Danh mục gói chuyên môn " + pack.Name() + ": " + ref.UsedFor,
		Detail:         types.JSON(detail),
	})
	if err != nil {
		return nil, fmt.Errorf("recording a request for %s: %w", ref.Number, err)
	}
	out.RequestID, out.RequestStatus, out.RequestCreated = row.ID, row.Status, true
	return out, nil
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

// document projects a catalogue row and notes when it no longer applies.
func (r *DomainPackReconciler) document(ctx context.Context, d *types.LegalDocument, now time.Time) (types.DomainPackReconcileDocument, error) {
	out := types.DomainPackReconcileDocument{
		ID: d.ID, DocumentNumber: d.DocumentNumber, Title: d.Title, DocType: d.DocType,
		Issuer: d.Issuer, Status: d.Status,
		EffectiveOn: reconcileDay(d.EffectiveOn), ExpiredOn: reconcileDay(d.ExpiredOn),
	}
	links, err := r.catalog.ListLinks(ctx, d.ID, nil,
		[]types.LegalRelation{types.LegalRelationReplaces, types.LegalRelationExpires})
	if err != nil {
		return out, fmt.Errorf("catalogue links of %s: %w", d.DocumentNumber, err)
	}
	out.StatusNote = reconcileStatusNote(d, links, now)
	return out, nil
}

// reconcileStatusNote says why a catalogue row the pack relies on may no
// longer apply: its status, its expiry date, or a document in force that
// replaced or ended it. "" when none of these holds.
func reconcileStatusNote(d *types.LegalDocument, links []types.LegalRelationLink, now time.Time) string {
	var notes []string
	status := strings.TrimSpace(d.Status)
	switch {
	case strings.HasPrefix(status, LegalExpiredStatusPrefix):
		notes = append(notes, "Danh mục ghi \""+status+"\"")
	case strings.EqualFold(status, "Hết hiệu lực một phần"):
		notes = append(notes, "Danh mục ghi \"Hết hiệu lực một phần\": kiểm tra phần gói dựa vào")
	}
	if d.ExpiredOn != nil && !d.ExpiredOn.After(now) {
		notes = append(notes, "ngày hết hiệu lực "+d.ExpiredOn.Format("02/01/2006"))
	}
	for _, l := range links {
		if !l.Incoming || l.Other == nil {
			continue
		}
		if l.Other.EffectiveOn != nil && l.Other.EffectiveOn.After(now) {
			continue // the replacement is not in force yet
		}
		verb := "bị thay thế bởi "
		if l.Relation == types.LegalRelationExpires {
			verb = "bị làm hết hiệu lực bởi "
		}
		note := verb + strings.TrimSpace(l.Other.DocumentNumber)
		if l.Other.EffectiveOn != nil {
			note += " (hiệu lực " + l.Other.EffectiveOn.Format("02/01/2006") + ")"
		}
		notes = append(notes, note)
	}
	return strings.Join(notes, "; ")
}

func reconcileDay(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.Format("2006-01-02")
}
