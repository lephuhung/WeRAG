package repository

import (
	"context"
	stderrors "errors"
	"sort"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// errTurnVersionStale marks a lost version race inside write transactions.
// Callers report it as (false, nil): a conflict, never a validation error.
var errTurnVersionStale = stderrors.New("abbreviation turn version race lost")

// isDomainAbsence reports whether err is a recognized domain outcome
// (missing/foreign/wrong-side source, scope mismatch, illegal transition)
// as opposed to an infrastructure failure, which must propagate and never
// be reinterpreted as absence.
func isDomainAbsence(err error) bool {
	return stderrors.Is(err, types.ErrAbbreviationNotFound) ||
		stderrors.Is(err, types.ErrAbbreviationConflict) ||
		stderrors.Is(err, types.ErrAbbreviationBadSelection)
}

// abbreviationTurnNow is the injected clock for turn expiry and timestamps.
// Tests override it (same package) instead of sleeping; production keeps
// time.Now.
var abbreviationTurnNow = time.Now

// abbreviationTurnTransitions is the turn lifecycle. Same-state entries are
// deliberate: partial replies persist on awaiting_definition, and metadata
// (sources, suggestion statuses, payload evidence) updates in place without
// changing lifecycle — including on completed answers, which never reopen
// QA. Blocked turns retry explicitly through inspecting, never by replaying
// running. Only ready claims running. Cancelled and expired are terminal and
// never reopen.
var abbreviationTurnTransitions = map[string]map[string]bool{
	types.AbbreviationTurnInspecting: {
		types.AbbreviationTurnInspecting:         true,
		types.AbbreviationTurnAwaitingDefinition: true,
		types.AbbreviationTurnReady:              true,
		types.AbbreviationTurnBlockedError:       true,
		types.AbbreviationTurnCancelled:          true,
		types.AbbreviationTurnExpired:            true,
	},
	types.AbbreviationTurnAwaitingDefinition: {
		types.AbbreviationTurnAwaitingDefinition: true,
		types.AbbreviationTurnReady:              true,
		types.AbbreviationTurnCancelled:          true,
		types.AbbreviationTurnExpired:            true,
	},
	types.AbbreviationTurnReady: {
		types.AbbreviationTurnReady:     true,
		types.AbbreviationTurnRunning:   true,
		types.AbbreviationTurnCancelled: true,
		types.AbbreviationTurnExpired:   true,
	},
	types.AbbreviationTurnRunning: {
		types.AbbreviationTurnRunning:      true,
		types.AbbreviationTurnCompleted:    true,
		types.AbbreviationTurnBlockedError: true,
		types.AbbreviationTurnCancelled:    true,
	},
	types.AbbreviationTurnBlockedError: {
		types.AbbreviationTurnBlockedError: true,
		types.AbbreviationTurnInspecting:   true,
		types.AbbreviationTurnCancelled:    true,
		types.AbbreviationTurnExpired:      true,
	},
	types.AbbreviationTurnCompleted: {
		types.AbbreviationTurnCompleted: true,
	},
}

var abbreviationTurnTerminal = map[string]bool{
	types.AbbreviationTurnCompleted: true,
	types.AbbreviationTurnCancelled: true,
	types.AbbreviationTurnExpired:   true,
}

// abbreviationTurnLiveStates lists every state cancellation may leave:
// the transition map's sources minus the terminal set, so the lifecycle
// table and cleanup scopes cannot drift apart.
func abbreviationTurnLiveStates() []string {
	var live []string
	for state := range abbreviationTurnTransitions {
		if !abbreviationTurnTerminal[state] {
			live = append(live, state)
		}
	}
	return live
}

func abbreviationTurnRoleValid(role string) bool {
	switch role {
	case types.AbbreviationMessageRoleRoot,
		types.AbbreviationMessageRoleDefinition,
		types.AbbreviationMessageRoleClarification,
		types.AbbreviationMessageRoleAnswer:
		return true
	default:
		return false
	}
}

// linkRoleClass pins each link role to its message side: root and definition
// evidence are user messages; clarification and answer traffic is assistant.
func abbreviationTurnLinkMessageRole(role string) (string, bool) {
	switch role {
	case types.AbbreviationMessageRoleRoot, types.AbbreviationMessageRoleDefinition:
		return "user", true
	case types.AbbreviationMessageRoleClarification, types.AbbreviationMessageRoleAnswer:
		return "assistant", true
	default:
		return "", false
	}
}

type abbreviationTurnRepository struct {
	db *gorm.DB
}

// NewAbbreviationTurnRepository creates the scoped clarification-turn repository.
func NewAbbreviationTurnRepository(db *gorm.DB) interfaces.AbbreviationTurnRepository {
	return &abbreviationTurnRepository{db: db}
}

// ownerScope filters every turn read/write to the full owner scope.
func ownerScope(db *gorm.DB, owner types.AbbreviationOwner) *gorm.DB {
	return db.Where("tenant_id = ? AND session_id = ? AND owner_id = ? AND principal_id = ?",
		owner.TenantID, owner.SessionID, owner.OwnerID, owner.PrincipalID)
}

// liveSessionRow is the ownership evidence of one session row.
type liveSessionRow struct {
	UserID string
}

// liveSession loads a live session of the owner tenant. A missing or
// soft-deleted session reports (nil, nil).
func (r *abbreviationTurnRepository) liveSession(ctx context.Context, db *gorm.DB, owner types.AbbreviationOwner) (*liveSessionRow, error) {
	var row liveSessionRow
	res := db.WithContext(ctx).Table("sessions").Select("user_id").
		Where("id = ? AND tenant_id = ? AND deleted_at IS NULL", owner.SessionID, owner.TenantID).
		First(&row)
	if res.Error != nil {
		if stderrors.Is(res.Error, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, res.Error
	}
	return &row, nil
}

// sessionOwnedBy binds the actual session owner to the requested owner.
// sessions.user_id carries principal-derived owner scope (human UUIDs as
// well as API/embed/IM owner strings); it must equal the requested owner
// exactly. No human UUID is invented: tenant-wide sessions (empty user_id)
// pair only with an empty owner.
func sessionOwnedBy(sess *liveSessionRow, owner types.AbbreviationOwner) bool {
	if sess == nil {
		return false
	}
	return sess.UserID == owner.OwnerID
}

// liveMessageRow is the placement evidence of one message row.
type liveMessageRow struct {
	SessionID string
	Role      string
}

// liveMessage loads a live message. A missing or soft-deleted message
// reports (nil, nil). Only real message columns are read; messages carry
// no tenant/owner/principal columns, so ownership always resolves through
// the message's live session.
func (r *abbreviationTurnRepository) liveMessage(ctx context.Context, db *gorm.DB, messageID string) (*liveMessageRow, error) {
	var row liveMessageRow
	res := db.WithContext(ctx).Table("messages").Select("session_id, role").
		Where("id = ? AND deleted_at IS NULL", messageID).First(&row)
	if res.Error != nil {
		if stderrors.Is(res.Error, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, res.Error
	}
	return &row, nil
}

// requireSessionOwnership enforces the actual live session and its owner
// binding. A missing or soft-deleted session is NotFound; a live session
// owned by someone else is a Conflict.
func (r *abbreviationTurnRepository) requireSessionOwnership(ctx context.Context, db *gorm.DB, owner types.AbbreviationOwner) error {
	sess, err := r.liveSession(ctx, db, owner)
	if err != nil {
		return err
	}
	if sess == nil {
		return types.ErrAbbreviationNotFound
	}
	if !sessionOwnedBy(sess, owner) {
		return types.ErrAbbreviationConflict
	}
	return nil
}

// requireSourceMessage enforces one source message: it must be live, belong
// to the owner session, and sit on the expected side (user/assistant).
// Missing or deleted sources are NotFound, foreign sessions Conflict, and
// wrong-side sources BadSelection.
func (r *abbreviationTurnRepository) requireSourceMessage(ctx context.Context, db *gorm.DB, owner types.AbbreviationOwner, messageID, wantRole string) error {
	msg, err := r.liveMessage(ctx, db, messageID)
	if err != nil {
		return err
	}
	if msg == nil {
		return types.ErrAbbreviationNotFound
	}
	if msg.SessionID != owner.SessionID {
		return types.ErrAbbreviationConflict
	}
	if msg.Role != wantRole {
		return types.ErrAbbreviationBadSelection
	}
	return nil
}

// lockTurnSession pins the session before writing a turn on PostgreSQL.
// The message lock follows the turn write, maintaining session/turn/message
// ordering so concurrent deletion cannot invalidate checked facts before
// commit. SQLite serializes writers at the database level; its reservation
// write remains the transaction's first statement, and post-write checks
// still apply on both dialects.
func lockTurnSession(tx *gorm.DB, owner types.AbbreviationOwner) error {
	if tx.Dialector.Name() != "postgres" {
		return nil
	}
	var sessionHit map[string]any
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Table("sessions").
		Where("id = ? AND tenant_id = ?", owner.SessionID, owner.TenantID).
		Take(&sessionHit).Error; err != nil && !stderrors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	return nil
}

func lockTurnDependencies(tx *gorm.DB, messageIDs []string) error {
	if tx.Dialector.Name() != "postgres" || len(messageIDs) == 0 {
		return nil
	}
	var messageHits []map[string]any
	return turnMessagesForUpdate(tx, messageIDs).Find(&messageHits).Error
}

func turnMessagesForUpdate(tx *gorm.DB, messageIDs []string) *gorm.DB {
	return tx.Clauses(clause.Locking{Strength: "UPDATE"}).Table("messages").Where("id IN ?", messageIDs).Order("id")
}

// dependencyMessageIDs gathers every message a write depends on: the root,
// stored definition links, proposed evidence, and executing references.
// The union covers stored sources a proposal replaces as well as incoming
// ones. Output is sorted; the PostgreSQL query orders rows before locking.
func dependencyMessageIDs(stored *types.AbbreviationTurnState, definitionLinks []types.AbbreviationMessageLink, proposed *types.AbbreviationTurnState, batch []types.AbbreviationMessageLink) []string {
	seen := map[string]bool{}
	var out []string
	add := func(id string) {
		if id == "" || seen[id] {
			return
		}
		seen[id] = true
		out = append(out, id)
	}
	addRow := func(row *types.AbbreviationTurnState) {
		if row == nil {
			return
		}
		add(row.RootUserMessageID)
		for _, id := range definitionSourceIDs(row.Payload) {
			add(id)
		}
		add(row.ClarificationMessageID)
		add(row.ExecutingMessageID)
	}
	addRow(stored)
	for _, link := range definitionLinks {
		add(link.MessageID)
	}
	addRow(proposed)
	for _, link := range batch {
		add(link.MessageID)
	}
	sort.Strings(out)
	return out
}

// requireProposedEvidence validates the value actually being persisted:
// definition evidence carried by the incoming payload plus any
// clarification/executing references. Evidence must be live user messages
// of the owner session; executing references must be live assistant
// messages. It complements requireTurnSources, which validates the
// already-stored sources.
func (r *abbreviationTurnRepository) requireProposedEvidence(ctx context.Context, db *gorm.DB, owner types.AbbreviationOwner, row *types.AbbreviationTurnState) error {
	for _, sourceID := range definitionSourceIDs(row.Payload) {
		if err := r.requireSourceMessage(ctx, db, owner, sourceID, "user"); err != nil {
			return err
		}
	}
	for _, executingID := range []string{row.ClarificationMessageID, row.ExecutingMessageID} {
		if executingID == "" {
			continue
		}
		if err := r.requireSourceMessage(ctx, db, owner, executingID, "assistant"); err != nil {
			return err
		}
	}
	return nil
}

// definitionSourceIDs collects the user-definition evidence pointers carried
// by the payload itself, so writes validate both the definition links and
// the evidence they must agree with.
func definitionSourceIDs(payload types.AbbreviationTurnPayload) []string {
	var out []string
	for i := range payload.Resolution.Terms {
		term := &payload.Resolution.Terms[i]
		if term.Source != types.AbbreviationSourceUserCurrent || term.Definition == nil {
			continue
		}
		out = append(out, term.Definition.SourceMessageID)
	}
	return out
}

// requireTurnSources enforces the root, every linked definition source, and
// every payload evidence pointer: all live user messages of the owner
// session. Used on reads (hide orphans) and before advancing writes.
func (r *abbreviationTurnRepository) requireTurnSources(ctx context.Context, db *gorm.DB, owner types.AbbreviationOwner, row *types.AbbreviationTurnState) error {
	if err := r.requireSourceMessage(ctx, db, owner, row.RootUserMessageID, "user"); err != nil {
		return err
	}
	var links []types.AbbreviationMessageLink
	if err := db.WithContext(ctx).
		Where("request_id = ? AND role = ?", row.ID, types.AbbreviationMessageRoleDefinition).
		Find(&links).Error; err != nil {
		return err
	}
	for i := range links {
		if err := r.requireSourceMessage(ctx, db, owner, links[i].MessageID, "user"); err != nil {
			return err
		}
	}
	for _, sourceID := range definitionSourceIDs(row.Payload) {
		if err := r.requireSourceMessage(ctx, db, owner, sourceID, "user"); err != nil {
			return err
		}
	}
	return nil
}

// snapshotsEqual compares frozen request snapshots with nil/empty slice
// equivalence, so JSON storage round-trips never read as scope changes.
func snapshotsEqual(a, b types.AbbreviationRequestSnapshot) bool {
	if a.AgentID != b.AgentID || a.Mode != b.Mode || a.AgentTenantID != b.AgentTenantID ||
		a.WebSearchEnabled != b.WebSearchEnabled || a.LocalBrowserEnabled != b.LocalBrowserEnabled ||
		a.Locale != b.Locale {
		return false
	}
	for _, pair := range [][2][]string{
		{a.KnowledgeBaseIDs, b.KnowledgeBaseIDs},
		{a.KnowledgeIDs, b.KnowledgeIDs},
		{a.MCPServiceIDs, b.MCPServiceIDs},
		{a.SkillNames, b.SkillNames},
		{a.AttachmentIDs, b.AttachmentIDs},
	} {
		if len(pair[0]) != len(pair[1]) {
			return false
		}
		for i := range pair[0] {
			if pair[0][i] != pair[1][i] {
				return false
			}
		}
	}
	if len(a.TagScopes) != len(b.TagScopes) {
		return false
	}
	for i := range a.TagScopes {
		if a.TagScopes[i].KnowledgeBaseID != b.TagScopes[i].KnowledgeBaseID {
			return false
		}
		ta, tb := a.TagScopes[i].TagIDs, b.TagScopes[i].TagIDs
		if len(ta) != len(tb) {
			return false
		}
		for j := range ta {
			if ta[j] != tb[j] {
				return false
			}
		}
	}
	return true
}

// Begin cancels the session's previous awaiting_definition turn and inserts
// the root inspecting turn in one transaction. The cancel write doubles as
// the SQLite write reservation: it is the transaction's first statement,
// so overlapping writers serialize instead of deadlocking on read-to-write
// lock upgrades (rolled back with everything else when validation fails).
// Locks are then acquired, the actual live session, its owner binding, the
// live user root message and the proposed payload evidence are validated,
// and ownership plus sources are re-verified after the insert — a failed
// Begin cancels nothing and an in-flight invalidation rolls back. Resume
// paths never call Begin: they load via Awaiting/Get and advance with
// CompareAndSwap.
func (r *abbreviationTurnRepository) Begin(ctx context.Context, owner types.AbbreviationOwner, row *types.AbbreviationTurnState) error {
	now := abbreviationTurnNow()
	if row == nil || row.ID == "" || owner.TenantID == 0 || owner.SessionID == "" ||
		owner.PrincipalID == "" || row.RootUserMessageID == "" {
		return types.ErrAbbreviationBadSelection
	}
	if row.State != types.AbbreviationTurnInspecting || row.Version != 1 {
		return types.ErrAbbreviationBadSelection
	}
	if row.ExpiresAt.IsZero() || !row.ExpiresAt.After(now) {
		return types.ErrAbbreviationBadSelection
	}
	if row.TenantID != owner.TenantID || row.SessionID != owner.SessionID ||
		row.OwnerID != owner.OwnerID || row.PrincipalID != owner.PrincipalID {
		return types.ErrAbbreviationConflict
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockTurnSession(tx, owner); err != nil {
			return err
		}
		if err := ownerScope(tx.Model(&types.AbbreviationTurnState{}), owner).
			Where("state = ?", types.AbbreviationTurnAwaitingDefinition).
			Updates(map[string]any{
				"state":      types.AbbreviationTurnCancelled,
				"version":    gorm.Expr("version + 1"),
				"updated_at": now,
			}).Error; err != nil {
			return err
		}
		if err := lockTurnDependencies(tx, dependencyMessageIDs(nil, nil, row, nil)); err != nil {
			return err
		}
		if err := r.requireSessionOwnership(ctx, tx, owner); err != nil {
			return err
		}
		if err := r.requireSourceMessage(ctx, tx, owner, row.RootUserMessageID, "user"); err != nil {
			return err
		}
		if err := r.requireProposedEvidence(ctx, tx, owner, row); err != nil {
			return err
		}
		row.CreatedAt = now
		row.UpdatedAt = now
		if err := tx.Create(row).Error; err != nil {
			return err
		}
		// Post-write re-verification, ownership included: facts
		// invalidated between the authorization point and the insert
		// roll the whole Begin back.
		if err := r.requireSessionOwnership(ctx, tx, owner); err != nil {
			return err
		}
		if err := r.requireSourceMessage(ctx, tx, owner, row.RootUserMessageID, "user"); err != nil {
			return err
		}
		return r.requireProposedEvidence(ctx, tx, owner, row)
	})
}

// Get returns one scoped turn whose session, owner binding, root and
// definition sources are all live. Recognized domain absence hides the
// turn; infrastructure failures propagate so callers never mistake an
// outage for a new request.
func (r *abbreviationTurnRepository) Get(ctx context.Context, owner types.AbbreviationOwner, id string) (*types.AbbreviationTurnState, error) {
	if id == "" {
		return nil, types.ErrAbbreviationNotFound
	}
	var row types.AbbreviationTurnState
	err := ownerScope(r.db.WithContext(ctx), owner).Where("id = ?", id).
		First(&row).Error
	if err != nil {
		if stderrors.Is(err, gorm.ErrRecordNotFound) {
			return nil, types.ErrAbbreviationNotFound
		}
		return nil, err
	}
	if err := r.requireSessionOwnership(ctx, r.db, owner); err != nil {
		if isDomainAbsence(err) {
			return nil, types.ErrAbbreviationNotFound
		}
		return nil, err
	}
	if err := r.requireTurnSources(ctx, r.db, owner, &row); err != nil {
		if isDomainAbsence(err) {
			return nil, types.ErrAbbreviationNotFound
		}
		return nil, err
	}
	return &row, nil
}

// Awaiting returns the session's live awaiting_definition turn whose expiry
// still lies in the future and whose sources are all live, or nil when
// there is none. Domain absence hides the turn; infrastructure failures
// propagate.
func (r *abbreviationTurnRepository) Awaiting(ctx context.Context, owner types.AbbreviationOwner, now time.Time) (*types.AbbreviationTurnState, error) {
	var row types.AbbreviationTurnState
	err := ownerScope(r.db.WithContext(ctx), owner).
		Where("state = ? AND expires_at > ?", types.AbbreviationTurnAwaitingDefinition, now).
		Order("updated_at DESC").First(&row).Error
	if err != nil {
		if stderrors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	if err := r.requireSessionOwnership(ctx, r.db, owner); err != nil {
		if isDomainAbsence(err) {
			return nil, nil
		}
		return nil, err
	}
	if err := r.requireTurnSources(ctx, r.db, owner, &row); err != nil {
		if isDomainAbsence(err) {
			return nil, nil
		}
		return nil, err
	}
	return &row, nil
}

// CompareAndSwap validates the transition, then swaps state, payload, error
// code and message links for the expected version, bumping it by one. The
// original root, owner scope, creation and expiry stamps, frozen request
// snapshot and original question are preserved: metadata writes cannot widen
// retrieval scope or silently replace the question. A lost version race or
// an out-of-scope row reports (false, nil); the caller retries against the
// fresh state instead of the repository inventing one.
//
// Isolation strategy: a reservation write opens the transaction before any
// reads, so overlapping SQLite writers serialize instead of deadlocking on
// read-to-write lock upgrades (the deployed DSN has no _txlock option and
// must not be changed here). The load, the ownership/source authorization
// checks and the conditional update follow in the same transaction; on
// PostgreSQL the depended session/message rows are SELECT ... FOR UPDATE-
// locked in sorted order. A post-write re-verification inside the same
// transaction rolls back anything invalidated between the authorization
// point and the update.
func (r *abbreviationTurnRepository) CompareAndSwap(ctx context.Context, owner types.AbbreviationOwner, expected uint64, row *types.AbbreviationTurnState) (bool, error) {
	now := abbreviationTurnNow()
	// The expected version is authoritative; the row's own Version field is
	// informational (callers reuse row structs across swaps, as in
	// read-modify-CAS loops), so it is not compared here.
	if row == nil || row.ID == "" || expected == 0 {
		return false, types.ErrAbbreviationBadSelection
	}
	if row.TenantID != owner.TenantID || row.SessionID != owner.SessionID ||
		row.OwnerID != owner.OwnerID || row.PrincipalID != owner.PrincipalID {
		return false, types.ErrAbbreviationConflict
	}
	var swapped bool
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockTurnSession(tx, owner); err != nil {
			return err
		}
		// Reservation-first: a matched-row real write before any reads, so
		// overlapping SQLite writers serialize instead of deadlocking on
		// read-to-write lock upgrades. Rolled back with everything else
		// when validation fails; overwritten by the real update below on
		// success. A stale or missing row reserves nothing and reports a
		// clean race loss after the load.
		if err := ownerScope(tx.Model(&types.AbbreviationTurnState{}), owner).
			Where("id = ? AND version = ?", row.ID, expected).
			Update("updated_at", now).Error; err != nil {
			return err
		}
		var current types.AbbreviationTurnState
		err := ownerScope(tx, owner).Where("id = ?", row.ID).First(&current).Error
		if err != nil {
			if stderrors.Is(err, gorm.ErrRecordNotFound) {
				return errTurnVersionStale
			}
			return err
		}
		if row.RootUserMessageID != current.RootUserMessageID {
			return types.ErrAbbreviationConflict
		}
		// A stale swap is a lost race, not a validation failure: report
		// the conflict without judging its transition.
		if current.Version != expected {
			return errTurnVersionStale
		}
		allowed, ok := abbreviationTurnTransitions[current.State]
		if !ok || !allowed[row.State] {
			return types.ErrAbbreviationBadSelection
		}
		// The waiting TTL authorizes resumption, not bookkeeping: past
		// the deadline only finalization (completed/cancelled/blocked
		// error) and expiry itself may proceed. Awaiting/ready/blocked
		// continuation, and any reopening of cancelled/expired turns
		// (which have no out-edges above), stay rejected.
		if pastDeadline := !now.Before(current.ExpiresAt); pastDeadline {
			switch row.State {
			case types.AbbreviationTurnExpired,
				types.AbbreviationTurnCompleted,
				types.AbbreviationTurnCancelled,
				types.AbbreviationTurnBlockedError:
			default:
				return types.ErrAbbreviationBadSelection
			}
		} else if row.State == types.AbbreviationTurnExpired {
			return types.ErrAbbreviationBadSelection
		}
		// Frozen scope and question survive every update.
		if !snapshotsEqual(current.Payload.Snapshot, row.Payload.Snapshot) {
			return types.ErrAbbreviationConflict
		}
		if row.Payload.Resolution.OriginalQuery != current.Payload.Resolution.OriginalQuery {
			return types.ErrAbbreviationConflict
		}
		var definitionLinks []types.AbbreviationMessageLink
		if err := tx.Where("request_id = ? AND role = ?", row.ID, types.AbbreviationMessageRoleDefinition).
			Find(&definitionLinks).Error; err != nil {
			return err
		}
		if err := lockTurnDependencies(tx, dependencyMessageIDs(&current, definitionLinks, row, nil)); err != nil {
			return err
		}
		if err := r.requireSessionOwnership(ctx, tx, owner); err != nil {
			return err
		}
		if err := r.requireTurnSources(ctx, tx, owner, &current); err != nil {
			return err
		}
		if err := r.requireProposedEvidence(ctx, tx, owner, row); err != nil {
			return err
		}
		res := ownerScope(tx.Model(&types.AbbreviationTurnState{}), owner).
			Where("id = ? AND version = ?", row.ID, expected).
			Updates(map[string]any{
				"state":                    row.State,
				"payload":                  row.Payload,
				"error_code":               row.ErrorCode,
				"clarification_message_id": row.ClarificationMessageID,
				"executing_message_id":     row.ExecutingMessageID,
				"version":                  expected + 1,
				"updated_at":               now,
			})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return errTurnVersionStale
		}
		// Post-write re-verification: facts invalidated between the
		// authorization point and the update roll the swap back. The
		// written value is verified, so newly proposed evidence is
		// covered as well as pre-existing sources.
		if err := r.requireSessionOwnership(ctx, tx, owner); err != nil {
			return err
		}
		written := current
		written.Payload = row.Payload
		if err := r.requireTurnSources(ctx, tx, owner, &written); err != nil {
			return err
		}
		for _, executingID := range []string{row.ClarificationMessageID, row.ExecutingMessageID} {
			if executingID == "" {
				continue
			}
			if err := r.requireSourceMessage(ctx, tx, owner, executingID, "assistant"); err != nil {
				return err
			}
		}
		swapped = true
		return nil
	})
	if stderrors.Is(err, errTurnVersionStale) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return swapped, nil
}

// LinkMessages transactionally links messages to a scoped turn under the
// same serialization discipline as Begin/CAS: a reservation write on the
// turn opens the transaction before any reads (advancing its activity
// timestamp, which is truthful link bookkeeping), depended rows are locked
// on PostgreSQL, and the session, root and every batch link are
// re-verified after the inserts — including exact idempotent retries, so a
// fault between validation and write rolls the batch back. The owning
// session and the turn's root must be live first. Every link names the same
// request, carries a known role on its message side (root/definition are
// user messages, clarification/answer are assistant messages), and points
// at a live message of the owner session; a root link must name the turn's
// actual root. An exact existing (message, request, role) retry is a no-op
// after the same checks; a different request or incompatible role for an
// existing message conflicts. Forks never copy links implicitly.
func (r *abbreviationTurnRepository) LinkMessages(ctx context.Context, owner types.AbbreviationOwner, requestID string, links []types.AbbreviationMessageLink) error {
	if requestID == "" || len(links) == 0 {
		return types.ErrAbbreviationBadSelection
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		now := abbreviationTurnNow()
		if err := lockTurnSession(tx, owner); err != nil {
			return err
		}
		if err := ownerScope(tx.Model(&types.AbbreviationTurnState{}), owner).
			Where("id = ?", requestID).Update("updated_at", now).Error; err != nil {
			return err
		}
		var turn types.AbbreviationTurnState
		err := ownerScope(tx, owner).Where("id = ?", requestID).First(&turn).Error
		if err != nil {
			if stderrors.Is(err, gorm.ErrRecordNotFound) {
				return types.ErrAbbreviationNotFound
			}
			return err
		}
		var storedLinks []types.AbbreviationMessageLink
		if err := tx.Where("request_id = ?", requestID).Find(&storedLinks).Error; err != nil {
			return err
		}
		if err := lockTurnDependencies(tx, dependencyMessageIDs(&turn, linksToDefinitions(storedLinks), &turn, links)); err != nil {
			return err
		}
		if err := r.requireSessionOwnership(ctx, tx, owner); err != nil {
			return err
		}
		if err := r.requireTurnSources(ctx, tx, owner, &turn); err != nil {
			return err
		}
		seen := map[string]types.AbbreviationMessageLink{}
		for i := range links {
			link := &links[i]
			if link.RequestID != requestID || link.MessageID == "" {
				return types.ErrAbbreviationBadSelection
			}
			wantRole, ok := abbreviationTurnLinkMessageRole(link.Role)
			if !ok {
				return types.ErrAbbreviationBadSelection
			}
			if link.Role == types.AbbreviationMessageRoleRoot && link.MessageID != turn.RootUserMessageID {
				return types.ErrAbbreviationBadSelection
			}
			if prev, dup := seen[link.MessageID]; dup {
				if prev.RequestID != link.RequestID || prev.Role != link.Role {
					return types.ErrAbbreviationConflict
				}
				continue
			}
			seen[link.MessageID] = *link
			if err := r.requireSourceMessage(ctx, tx, owner, link.MessageID, wantRole); err != nil {
				return err
			}
			var existing types.AbbreviationMessageLink
			err := tx.Where("message_id = ?", link.MessageID).First(&existing).Error
			if err == nil {
				if existing.RequestID != link.RequestID || existing.Role != link.Role {
					return types.ErrAbbreviationConflict
				}
				continue
			}
			if !stderrors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}
			if err := tx.Create(link).Error; err != nil {
				return err
			}
		}
		// Post-write re-verification covers inserts and exact idempotent
		// retries alike: session, root and every batch link must still be
		// live, or the batch rolls back.
		if err := r.requireSessionOwnership(ctx, tx, owner); err != nil {
			return err
		}
		if err := r.requireTurnSources(ctx, tx, owner, &turn); err != nil {
			return err
		}
		for i := range links {
			wantRole, ok := abbreviationTurnLinkMessageRole(links[i].Role)
			if !ok {
				return types.ErrAbbreviationBadSelection
			}
			if err := r.requireSourceMessage(ctx, tx, owner, links[i].MessageID, wantRole); err != nil {
				return err
			}
		}
		return nil
	})
}

// linksToDefinitions filters a batch down to its definition links for
// dependency locking.
func linksToDefinitions(links []types.AbbreviationMessageLink) []types.AbbreviationMessageLink {
	var out []types.AbbreviationMessageLink
	for _, link := range links {
		if link.Role == types.AbbreviationMessageRoleDefinition {
			out = append(out, link)
		}
	}
	return out
}

// ByMessages resolves the scoped turns linked to live owner-session
// messages, keyed by message ID. Turns whose session, root or definition
// sources are not all live are excluded. Each key gets an independent
// deep copy of the payload. Infrastructure failures abort the whole call:
// no partial-success map is ever returned.
func (r *abbreviationTurnRepository) ByMessages(ctx context.Context, owner types.AbbreviationOwner, messageIDs []string) (map[string]*types.AbbreviationTurnState, error) {
	out := map[string]*types.AbbreviationTurnState{}
	if len(messageIDs) == 0 {
		return out, nil
	}
	if err := r.requireSessionOwnership(ctx, r.db, owner); err != nil {
		if isDomainAbsence(err) {
			return out, nil
		}
		return nil, err
	}
	var links []types.AbbreviationMessageLink
	if err := r.db.WithContext(ctx).Where("message_id IN ?", messageIDs).Find(&links).Error; err != nil {
		return nil, err
	}
	if len(links) == 0 {
		return out, nil
	}
	requestIDs := make([]string, 0, len(links))
	byRequest := map[string][]string{}
	for _, link := range links {
		byRequest[link.RequestID] = append(byRequest[link.RequestID], link.MessageID)
	}
	for id := range byRequest {
		requestIDs = append(requestIDs, id)
	}
	var states []types.AbbreviationTurnState
	if err := ownerScope(r.db.WithContext(ctx), owner).Where("id IN ?", requestIDs).Find(&states).Error; err != nil {
		return nil, err
	}
	byID := map[string]*types.AbbreviationTurnState{}
	for i := range states {
		byID[states[i].ID] = &states[i]
	}
	for requestID, mids := range byRequest {
		state, ok := byID[requestID]
		if !ok {
			continue
		}
		if err := r.requireTurnSources(ctx, r.db, owner, state); err != nil {
			if isDomainAbsence(err) {
				continue
			}
			return nil, err
		}
		for _, mid := range mids {
			live, err := r.liveMessage(ctx, r.db, mid)
			if err != nil {
				return nil, err
			}
			if live == nil || live.SessionID != owner.SessionID {
				continue
			}
			cp := *state
			cp.Payload = state.Payload.Clone()
			out[mid] = &cp
		}
	}
	return out, nil
}

// cancelScoped moves the owner session's non-terminal turns to cancelled.
// Terminal turns (completed, cancelled, expired) are left untouched, so
// cleanup never reopens finished work — but it also never depends on the
// orphan's sources being live: cancellation is ownership-scoped only.
func (r *abbreviationTurnRepository) cancelScoped(ctx context.Context, tx *gorm.DB, owner types.AbbreviationOwner) error {
	return ownerScope(tx.Model(&types.AbbreviationTurnState{}), owner).
		Where("state IN ?", abbreviationTurnLiveStates()).
		Updates(map[string]any{
			"state":      types.AbbreviationTurnCancelled,
			"version":    gorm.Expr("version + 1"),
			"updated_at": abbreviationTurnNow(),
		}).Error
}

// CancelByMessages cancels the non-terminal turns linked to the messages.
func (r *abbreviationTurnRepository) CancelByMessages(ctx context.Context, owner types.AbbreviationOwner, messageIDs []string) error {
	if len(messageIDs) == 0 {
		return nil
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var requestIDs []string
		if err := tx.Model(&types.AbbreviationMessageLink{}).
			Where("message_id IN ?", messageIDs).
			Distinct().Pluck("request_id", &requestIDs).Error; err != nil {
			return err
		}
		if len(requestIDs) == 0 {
			return nil
		}
		return ownerScope(tx.Model(&types.AbbreviationTurnState{}), owner).
			Where("id IN ? AND state IN ?", requestIDs, abbreviationTurnLiveStates()).
			Updates(map[string]any{
				"state":      types.AbbreviationTurnCancelled,
				"version":    gorm.Expr("version + 1"),
				"updated_at": abbreviationTurnNow(),
			}).Error
	})
}

// CancelSession cancels every non-terminal turn of the owner session.
func (r *abbreviationTurnRepository) CancelSession(ctx context.Context, owner types.AbbreviationOwner) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return r.cancelScoped(ctx, tx, owner)
	})
}
