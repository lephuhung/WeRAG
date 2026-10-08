package service

import (
	"context"
	"sort"
	"sync"
	"testing"
	"time"

	apperrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

type dwFakeRevisions struct {
	mu   sync.Mutex
	rows []types.DocumentRevision
}

func (r *dwFakeRevisions) Create(_ context.Context, rev *types.DocumentRevision) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	maxSeq := 0
	for _, row := range r.rows {
		if row.WorkspaceID == rev.WorkspaceID && row.Seq > maxSeq {
			maxSeq = row.Seq
		}
	}
	rev.Seq = maxSeq + 1
	_ = rev.BeforeCreate(nil)
	rev.CreatedAt = time.Now()
	r.rows = append(r.rows, *rev)
	return nil
}

func (r *dwFakeRevisions) ListByWorkspace(_ context.Context, workspaceID string) ([]*types.DocumentRevision, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []*types.DocumentRevision
	for i := range r.rows {
		if r.rows[i].WorkspaceID == workspaceID {
			cp := r.rows[i]
			out = append(out, &cp)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Seq > out[j].Seq })
	return out, nil
}

func (r *dwFakeRevisions) GetBySeq(_ context.Context, workspaceID string, seq int) (*types.DocumentRevision, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, row := range r.rows {
		if row.WorkspaceID == workspaceID && row.Seq == seq {
			cp := row
			return &cp, nil
		}
	}
	return nil, nil
}

func (r *dwFakeRevisions) Latest(ctx context.Context, workspaceID string) (*types.DocumentRevision, error) {
	list, _ := r.ListByWorkspace(ctx, workspaceID)
	if len(list) == 0 {
		return nil, nil
	}
	return list[0], nil
}

func TestDocumentRevisionSnapshotDedupe(t *testing.T) {
	fx := newDWFixture(t)
	ws := fx.create(t)
	fx.ds.onCommand = func(map[string]interface{}) int { return onlyOfficeCommandNoChanges }

	first, err := fx.svc.Snapshot(context.Background(), 7, "sess-1", "", "Bản đầu", types.DocumentRevisionSourceManual, time.Second)
	require.NoError(t, err)
	require.Equal(t, 1, first.Seq)
	require.Equal(t, ws.CurrentRef, first.Ref)
	require.Equal(t, "Bản đầu", first.Label)
	require.Equal(t, types.DocumentRevisionSourceManual, first.Source)

	// Nothing changed: the same row comes back.
	again, err := fx.svc.Snapshot(context.Background(), 7, "sess-1", "", "AI: sửa điều 3", types.DocumentRevisionSourceAI, time.Second)
	require.NoError(t, err)
	require.Equal(t, first.ID, again.ID)
	require.Len(t, fx.revs.rows, 1)

	// The forcesave carried the label in userdata.
	last := fx.ds.commands[len(fx.ds.commands)-1]
	require.Equal(t, "werag:"+ws.ID+"|ai:AI: sửa điều 3", last["userdata"])

	_, err = fx.svc.Snapshot(context.Background(), 7, "sess-1", "", "x", "bogus", time.Second)
	requireAppCode(t, err, apperrors.ErrBadRequest)
}

func TestDocumentRevisionSnapshotAfterForceSave(t *testing.T) {
	fx := newDWFixture(t)
	ws := fx.create(t)
	ticket := fx.ticket(t, ws)
	savedURL := fx.ds.put("edited.docx", []byte("PK-edited"))
	fx.ds.onCommand = func(body map[string]interface{}) int {
		go func() {
			cb := &types.OnlyOfficeCallback{Key: body["key"].(string), Status: 6, URL: savedURL,
				UserData: body["userdata"].(string)}
			_ = fx.svc.HandleCallback(context.Background(), ticket, dwBearer(t, dwTestSecret, cb), cb)
		}()
		return onlyOfficeCommandOK
	}

	rev, err := fx.svc.Snapshot(context.Background(), 7, "sess-1", "", "Trước khi AI sửa", types.DocumentRevisionSourceAI, 5*time.Second)
	require.NoError(t, err)
	got, _ := fx.repo.GetByID(context.Background(), ws.ID)
	require.Equal(t, got.CurrentRef, rev.Ref, "snapshot points at the freshly saved file")
	require.Equal(t, []byte("PK-edited"), fx.files.get(rev.Ref))
	require.Equal(t, types.DocumentRevisionSourceAI, rev.Source)
	require.Equal(t, "Trước khi AI sửa", rev.Label)
	require.Len(t, fx.revs.rows, 1, "callback row and Snapshot row are the same")
}

func TestDocumentRevisionCallbackUserdataLabel(t *testing.T) {
	fx := newDWFixture(t)
	ws := fx.create(t)

	// Labelled manual save.
	url := fx.ds.put("m.docx", []byte("PK-manual"))
	cb := &types.OnlyOfficeCallback{Key: ws.EditorKey(), Status: 6, URL: url, UserData: "werag:" + ws.ID + "|Bản nháp 1"}
	require.NoError(t, fx.svc.HandleCallback(context.Background(), fx.ticket(t, ws), dwBearer(t, dwTestSecret, cb), cb))
	// Labelled AI save.
	url = fx.ds.put("a.docx", []byte("PK-ai"))
	cb = &types.OnlyOfficeCallback{Key: ws.EditorKey(), Status: 6, URL: url, UserData: "werag:" + ws.ID + "|ai:Sửa thể thức"}
	require.NoError(t, fx.svc.HandleCallback(context.Background(), fx.ticket(t, ws), dwBearer(t, dwTestSecret, cb), cb))
	// Unlabelled autosave: no row.
	url = fx.ds.put("auto.docx", []byte("PK-auto"))
	cb = &types.OnlyOfficeCallback{Key: ws.EditorKey(), Status: 6, URL: url, UserData: "werag:" + ws.ID}
	require.NoError(t, fx.svc.HandleCallback(context.Background(), fx.ticket(t, ws), dwBearer(t, dwTestSecret, cb), cb))
	// Userdata of another workspace is not trusted as a label.
	url = fx.ds.put("other.docx", []byte("PK-other"))
	cb = &types.OnlyOfficeCallback{Key: ws.EditorKey(), Status: 6, URL: url, UserData: "werag:other|ai:x"}
	require.NoError(t, fx.svc.HandleCallback(context.Background(), fx.ticket(t, ws), dwBearer(t, dwTestSecret, cb), cb))
	// Final save: a "close" row.
	url = fx.ds.put("close.docx", []byte("PK-close"))
	cb = &types.OnlyOfficeCallback{Key: ws.EditorKey(), Status: 2, URL: url}
	require.NoError(t, fx.svc.HandleCallback(context.Background(), fx.ticket(t, ws), dwBearer(t, dwTestSecret, cb), cb))

	list, err := fx.svc.ListRevisions(context.Background(), 7, "sess-1", "")
	require.NoError(t, err)
	require.Len(t, list, 3)
	require.Equal(t, []int{3, 2, 1}, []int{list[0].Seq, list[1].Seq, list[2].Seq}, "newest first")
	require.Equal(t, types.DocumentRevisionSourceClose, list[0].Source)
	require.Equal(t, "Đóng tài liệu", list[0].Label)
	require.Equal(t, []byte("PK-close"), fx.files.get(list[0].Ref))
	require.Equal(t, types.DocumentRevisionSourceAI, list[1].Source)
	require.Equal(t, "Sửa thể thức", list[1].Label)
	require.Equal(t, []byte("PK-ai"), fx.files.get(list[1].Ref))
	require.Equal(t, types.DocumentRevisionSourceManual, list[2].Source)
	require.Equal(t, "Bản nháp 1", list[2].Label)
}

func TestDocumentRevisionRestoreRotatesKey(t *testing.T) {
	fx := newDWFixture(t)
	ws := fx.create(t)
	fx.ds.onCommand = func(map[string]interface{}) int { return onlyOfficeCommandNoChanges }
	original, err := fx.svc.Snapshot(context.Background(), 7, "sess-1", "", "Gốc", types.DocumentRevisionSourceManual, time.Second)
	require.NoError(t, err)

	// The user edits in the editor (saved through a callback).
	url := fx.ds.put("edit.docx", []byte("PK-edit"))
	cb := &types.OnlyOfficeCallback{Key: ws.EditorKey(), Status: 6, URL: url}
	require.NoError(t, fx.svc.HandleCallback(context.Background(), fx.ticket(t, ws), dwBearer(t, dwTestSecret, cb), cb))
	edited, _ := fx.repo.GetByID(context.Background(), ws.ID)

	restored, err := fx.svc.Restore(context.Background(), 7, "sess-1", "", original.Seq)
	require.NoError(t, err)
	require.Equal(t, original.Ref, restored.CurrentRef)
	require.Equal(t, 1, restored.Revision)
	require.Equal(t, ws.ID+"-1", restored.EditorKey(), "new key makes the editor reload")

	list, _ := fx.svc.ListRevisions(context.Background(), 7, "sess-1", "")
	require.Len(t, list, 3)
	require.Equal(t, "Khôi phục bản #1", list[0].Label)
	require.Equal(t, types.DocumentRevisionSourceRestore, list[0].Source)
	require.Equal(t, original.Ref, list[0].Ref)
	require.Equal(t, "Trước khi khôi phục", list[1].Label)
	require.Equal(t, types.DocumentRevisionSourceRestore, list[1].Source)
	require.Equal(t, edited.CurrentRef, list[1].Ref, "the replaced edit stays restorable")

	_, err = fx.svc.Restore(context.Background(), 7, "sess-1", "", 99)
	requireAppCode(t, err, apperrors.ErrNotFound)
}

func TestSnapshotUserdataRoundTrip(t *testing.T) {
	for _, tc := range []struct{ label, source, wantLabel, wantSource string }{
		{"Bản 1", "manual", "Bản 1", "manual"},
		{"ai: trông như AI", "manual", "ai: trông như AI", "manual"},
		{"Sửa | điều 3", "ai", "Sửa / điều 3", "ai"},
		{"Trước khi khôi phục", "restore", "Trước khi khôi phục", "restore"},
	} {
		label, source, ok := parseSnapshotUserdata("ws", "werag:ws|"+encodeSnapshotTag(tc.label, tc.source))
		require.True(t, ok)
		require.Equal(t, tc.wantLabel, label)
		require.Equal(t, tc.wantSource, source)
	}
	_, _, ok := parseSnapshotUserdata("ws", "werag:ws")
	require.False(t, ok)
}
