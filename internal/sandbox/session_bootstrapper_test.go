package sandbox

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

var testSessionKey = SessionSandboxKey{TenantID: 1, SessionID: "session-bootstrap"}

type recordingBootstrapper struct {
	override      string
	overrideErr   error
	afterErr      error
	afterCalls    int
	afterHandleID string
}

func (b *recordingBootstrapper) TemplateOverride(
	context.Context, SessionSandboxKey,
) (string, error) {
	return b.override, b.overrideErr
}

func (b *recordingBootstrapper) AfterCreate(
	_ context.Context, _ SessionSandboxKey, handle RemoteSandboxHandle,
) error {
	b.afterCalls++
	if handle != nil {
		b.afterHandleID = handle.ID()
	}
	return b.afterErr
}

func newLifecycleForTest(t *testing.T) (*remoteSessionLifecycle, *fakeRemoteClient, SessionSandboxBindingStore) {
	t.Helper()
	client := newFakeRemoteClient(SandboxTypeCube)
	store := NewMemorySessionSandboxBindingStore()
	lc := newTestRemoteSessionLifecycle(
		t, client, store, &fakeSessionExistenceChecker{exists: true},
	)
	return lc, client, store
}

func TestCreateAndBindUsesTemplateOverride(t *testing.T) {
	lc, client, _ := newLifecycleForTest(t)
	lc.bootstrapper = &recordingBootstrapper{override: "snap-1"}

	_, err := lc.Resolve(context.Background(), testSessionKey)

	require.NoError(t, err)
	require.Equal(t, "snap-1", client.lastCreateRequest.TemplateID)
}

func TestCreateAndBindKeepsConfigTemplateWithoutOverride(t *testing.T) {
	lc, client, _ := newLifecycleForTest(t)
	lc.bootstrapper = &recordingBootstrapper{override: ""}

	_, err := lc.Resolve(context.Background(), testSessionKey)

	require.NoError(t, err)
	require.Equal(t, lc.createRequest.TemplateID, client.lastCreateRequest.TemplateID)
}

// Điều quan trọng nhất: override tuyệt đối không được làm bẩn l.createRequest dùng chung giữa các cuộc trò chuyện.
func TestTemplateOverrideDoesNotMutateSharedCreateRequest(t *testing.T) {
	lc, _, _ := newLifecycleForTest(t)
	baseTemplate := lc.createRequest.TemplateID
	lc.bootstrapper = &recordingBootstrapper{override: "snap-1"}

	_, err := lc.Resolve(context.Background(), testSessionKey)

	require.NoError(t, err)
	require.Equal(t, baseTemplate, lc.createRequest.TemplateID,
		"createRequest dùng chung đã bị sửa; các cuộc trò chuyện khác cùng config sẽ khởi động từ snapshot của người khác")
}

func TestAfterCreateRunsWithTheNewHandle(t *testing.T) {
	lc, _, _ := newLifecycleForTest(t)
	b := &recordingBootstrapper{override: "snap-1"}
	lc.bootstrapper = b

	handle, err := lc.Resolve(context.Background(), testSessionKey)

	require.NoError(t, err)
	require.Equal(t, 1, b.afterCalls)
	require.Equal(t, handle.ID(), b.afterHandleID)
}

// Tất cả hoặc không gì cả: bootstrap thất bại phải hủy sandbox và xóa binding, nếu không người dùng sẽ làm việc
// trong một sandbox mà "hệ thống tệp dừng ở thời điểm rẽ nhánh chứ không phải điểm rẽ nhánh".
func TestAfterCreateFailureDestroysSandboxAndBinding(t *testing.T) {
	lc, client, store := newLifecycleForTest(t)
	lc.bootstrapper = &recordingBootstrapper{override: "snap-1", afterErr: errors.New("git reset failed")}

	_, err := lc.Resolve(context.Background(), testSessionKey)

	require.Error(t, err)
	require.Contains(t, client.deleteIDs, client.lastCreatedID)

	binding, getErr := store.Get(context.Background(), testSessionKey)
	require.NoError(t, getErr)
	require.Nil(t, binding, "bootstrap thất bại thì không được để lại binding")
}

func TestTemplateOverrideErrorAbortsCreate(t *testing.T) {
	lc, client, _ := newLifecycleForTest(t)
	lc.bootstrapper = &recordingBootstrapper{overrideErr: errors.New("db down")}

	_, err := lc.Resolve(context.Background(), testSessionKey)

	require.Error(t, err)
	require.Empty(t, client.lastCreatedID, "không đọc được override thì không được tạo sandbox")
}

type recordingCreateFailureHandler struct {
	recordingBootstrapper
	failedCalls int
	lastFailErr error
}

func (b *recordingCreateFailureHandler) OnCreateFailed(_ context.Context, _ SessionSandboxKey, err error) {
	b.failedCalls++
	b.lastFailErr = err
}

func TestNilBootstrapperIsANoOp(t *testing.T) {
	lc, client, _ := newLifecycleForTest(t)
	lc.bootstrapper = nil

	_, err := lc.Resolve(context.Background(), testSessionKey)

	require.NoError(t, err)
	require.Equal(t, lc.createRequest.TemplateID, client.lastCreateRequest.TemplateID)
}

// A missing fork snapshot must not be retried forever. Create using the
// override failed, so the bootstrapper has to be told — AfterCreate never
// runs, and abandon on git-reset-failure cannot fire.
func TestCreateFailureWithOverrideNotifiesBootstrapper(t *testing.T) {
	lc, client, _ := newLifecycleForTest(t)
	handler := &recordingCreateFailureHandler{
		recordingBootstrapper: recordingBootstrapper{override: "snap-1"},
	}
	lc.bootstrapper = handler
	client.createErr = NewRemoteError(
		SandboxTypeCube, "Create", RemoteErrorKindNotFound, "template gone", nil,
	)

	_, err := lc.Resolve(context.Background(), testSessionKey)

	require.Error(t, err)
	require.Equal(t, 1, handler.failedCalls)
	require.True(t, IsRemoteNotFound(handler.lastFailErr))
	require.Empty(t, client.lastCreatedID)
}

func TestCreateFailureWithoutOverrideDoesNotNotifyBootstrapper(t *testing.T) {
	lc, client, _ := newLifecycleForTest(t)
	handler := &recordingCreateFailureHandler{}
	lc.bootstrapper = handler
	client.createErr = NewRemoteError(
		SandboxTypeCube, "Create", RemoteErrorKindNotFound, "template gone", nil,
	)

	_, err := lc.Resolve(context.Background(), testSessionKey)

	require.Error(t, err)
	require.Zero(t, handler.failedCalls)
}

func TestTransientCreateFailureWithOverrideStillNotifiesBootstrapper(t *testing.T) {
	lc, client, _ := newLifecycleForTest(t)
	handler := &recordingCreateFailureHandler{
		recordingBootstrapper: recordingBootstrapper{override: "snap-1"},
	}
	lc.bootstrapper = handler
	client.createErr = NewRemoteError(
		SandboxTypeCube, "Create", RemoteErrorKindUnavailable, "provider down", nil,
	)

	_, err := lc.Resolve(context.Background(), testSessionKey)

	require.Error(t, err)
	require.Equal(t, 1, handler.failedCalls)
}
