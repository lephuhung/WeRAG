package service

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/types"
)

// fragmented and healthy are reports as read() would build them on
// PostgreSQL. They are hand-made here because the unit-test dialect has no
// paradedb.index_info to read, so alert() is exercised directly.
func fragmentedReport() SearchIndexHealthReport {
	return SearchIndexHealthReport{
		Index: SearchIndexName, Available: true,
		Segments: 187, Threshold: 60, DeletedDocs: 20607, NeedsRebuild: true,
	}
}

func healthyReport() SearchIndexHealthReport {
	return SearchIndexHealthReport{
		Index: SearchIndexName, Available: true,
		Segments: 20, Threshold: 60, NeedsRebuild: false,
	}
}

// withAlertEnv wires the check to the test database's SuperAdmins.
func withAlertEnv(t *testing.T) (*queueNoticeEnv, *SearchIndexHealth) {
	t.Helper()
	env := newQueueNoticeEnv(t, nil)
	h := NewSearchIndexHealth(env.db, nil).WithAlerts(
		repository.NewUserRepository(env.db), env.notices, nil)
	require.True(t, h.alerting(), "WithAlerts should have attached the digest")
	return env, h
}

func searchIndexPayload(t *testing.T, env *queueNoticeEnv, user string) types.UserNotificationSearchIndexAlertPayload {
	t.Helper()
	n := env.unread(t, user, types.UserNotificationSearchIndexAlert)
	require.NotNil(t, n, "%s has an unread search_index_alert", user)
	var p types.UserNotificationSearchIndexAlertPayload
	require.NoError(t, json.Unmarshal(n.Payload, &p))
	return p
}

func TestSearchIndexAlertTellsEverySuperAdmin(t *testing.T) {
	env, h := withAlertEnv(t)

	h.alert(context.Background(), fragmentedReport())

	for _, sa := range []string{"sa1", "sa2"} {
		n := env.unread(t, sa, types.UserNotificationSearchIndexAlert)
		require.NotNil(t, n, "%s was not told", sa)
		require.Equal(t, SearchIndexAlertLink, n.Link)
		require.NotEmpty(t, n.Title)
		require.Contains(t, n.Body, "187")

		p := searchIndexPayload(t, env, sa)
		require.Equal(t, SearchIndexName, p.Index)
		require.Equal(t, 187, p.Segments)
		require.Equal(t, 60, p.Threshold)
		require.Equal(t, int64(20607), p.DeletedDocs)
		// The signature names the index, not the count: segments creep up
		// while an incident lasts and must not re-toast it.
		require.Equal(t, SearchIndexName+":fragmented", p.Signature)
		require.Nil(t, p.ResolvedAt)
	}
	env.nobodyElse(t)
}

// A count that keeps rising is the same incident, so the SuperAdmin must not
// be toasted again (env.unread also asserts a single unread per kind).
func TestSearchIndexAlertDoesNotRepeatTheSameIncident(t *testing.T) {
	env, h := withAlertEnv(t)

	h.alert(context.Background(), fragmentedReport())
	worse := fragmentedReport()
	worse.Segments = 240
	h.alert(context.Background(), worse)

	p := searchIndexPayload(t, env, "sa1")
	require.Equal(t, 187, p.Segments, "the open digest is left as it was raised")
}

func TestSearchIndexAlertResolvesOnceHealthy(t *testing.T) {
	env, h := withAlertEnv(t)
	h.alert(context.Background(), fragmentedReport())
	require.NotNil(t, env.unread(t, "sa1", types.UserNotificationSearchIndexAlert))

	h.alert(context.Background(), healthyReport())

	env.closed(t, types.UserNotificationSearchIndexAlert)
}

// A report that could not be read decides nothing: it is neither an incident
// nor evidence that one ended. Resolving on it would quietly close an open
// alert the moment the database went unreachable.
func TestSearchIndexAlertIgnoresAnUnreadableReport(t *testing.T) {
	env, h := withAlertEnv(t)
	h.alert(context.Background(), fragmentedReport())

	h.alert(context.Background(), SearchIndexHealthReport{
		Index: SearchIndexName, Reason: SearchIndexReasonQueryFailed,
	})

	require.NotNil(t, env.unread(t, "sa1", types.UserNotificationSearchIndexAlert),
		"an unreadable report must not close the open alert")
}

func TestSearchIndexAlertSkippedWhenNotWired(t *testing.T) {
	env := newQueueNoticeEnv(t, nil)
	h := NewSearchIndexHealth(env.db, nil) // no WithAlerts
	require.False(t, h.alerting())

	require.NotPanics(t, func() { h.alert(context.Background(), fragmentedReport()) })

	require.Nil(t, env.unread(t, "sa1", types.UserNotificationSearchIndexAlert))
}

// WithAlerts must tolerate the half-wired shapes a container can hand it.
func TestSearchIndexWithAlertsRequiresBothRepositories(t *testing.T) {
	env := newQueueNoticeEnv(t, nil)

	require.False(t, NewSearchIndexHealth(env.db, nil).
		WithAlerts(nil, env.notices, nil).alerting())
	require.False(t, NewSearchIndexHealth(env.db, nil).
		WithAlerts(repository.NewUserRepository(env.db), nil, nil).alerting())

	var nilCheck *SearchIndexHealth
	require.Nil(t, nilCheck.WithAlerts(repository.NewUserRepository(env.db), env.notices, nil))
	require.False(t, nilCheck.alerting())
}

// Tick is what the ticker calls; on the unit-test dialect the report is
// unavailable, so it must log and leave the digest alone rather than panic.
func TestSearchIndexTickWithAlertsOnUnavailableReport(t *testing.T) {
	env, h := withAlertEnv(t)

	require.NotPanics(t, func() {
		rep := h.Tick(context.Background())
		require.False(t, rep.Available)
	})

	require.Nil(t, env.unread(t, "sa1", types.UserNotificationSearchIndexAlert))
}
