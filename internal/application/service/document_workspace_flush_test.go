package service

import (
	"context"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

// SnapshotSaved says whether the editor's save landed; Snapshot (the
// format check's path) still proceeds without an error in every case.
func TestSnapshotSavedReportsWhetherTheSaveLanded(t *testing.T) {
	ctx := context.Background()
	snap := func(fx *dwFixture, wait time.Duration) (bool, error) {
		_, saved, err := fx.svc.SnapshotSaved(ctx, 7, "sess-1", "", "ai: rà soát", types.DocumentRevisionSourceAI, wait)
		return saved, err
	}

	t.Run("nothing to save", func(t *testing.T) {
		fx := newDWFixture(t)
		fx.create(t)
		fx.ds.onCommand = func(map[string]interface{}) int { return onlyOfficeCommandNoChanges }
		saved, err := snap(fx, time.Second)
		require.NoError(t, err)
		require.True(t, saved)
	})

	t.Run("callback arrives", func(t *testing.T) {
		fx := newDWFixture(t)
		ws := fx.create(t)
		ticket := fx.ticket(t, ws)
		url := fx.ds.put("edited.docx", []byte("PK-edited"))
		fx.ds.onCommand = func(body map[string]interface{}) int {
			go func() {
				cb := &types.OnlyOfficeCallback{Key: body["key"].(string), Status: 6, URL: url, UserData: body["userdata"].(string)}
				_ = fx.svc.HandleCallback(context.Background(), ticket, dwBearer(t, dwTestSecret, cb), cb)
			}()
			return onlyOfficeCommandOK
		}
		saved, err := snap(fx, 5*time.Second)
		require.NoError(t, err)
		require.True(t, saved)
	})

	t.Run("force-save command fails", func(t *testing.T) {
		fx := newDWFixture(t)
		fx.create(t)
		fx.ds.onCommand = func(map[string]interface{}) int { return 6 } // invalid token
		saved, err := snap(fx, time.Second)
		require.NoError(t, err)
		require.False(t, saved)
		_, err = fx.svc.Snapshot(ctx, 7, "sess-1", "", "ai: thể thức", types.DocumentRevisionSourceAI, time.Second)
		require.NoError(t, err, "the format check still proceeds on the stored version")
	})

	t.Run("callback never arrives", func(t *testing.T) {
		fx := newDWFixture(t)
		fx.create(t)
		fx.ds.onCommand = func(map[string]interface{}) int { return onlyOfficeCommandOK }
		start := time.Now()
		saved, err := snap(fx, 200*time.Millisecond)
		require.NoError(t, err)
		require.False(t, saved)
		require.Less(t, time.Since(start), 3*time.Second)
	})

	t.Run("document server reports a save error", func(t *testing.T) {
		fx := newDWFixture(t)
		ws := fx.create(t)
		ticket := fx.ticket(t, ws)
		fx.ds.onCommand = func(body map[string]interface{}) int {
			go func() {
				cb := &types.OnlyOfficeCallback{Key: body["key"].(string), Status: onlyOfficeStatusForceSaveError}
				_ = fx.svc.HandleCallback(context.Background(), ticket, dwBearer(t, dwTestSecret, cb), cb)
			}()
			return onlyOfficeCommandOK
		}
		saved, err := snap(fx, 5*time.Second)
		require.NoError(t, err)
		require.False(t, saved)
	})
}
