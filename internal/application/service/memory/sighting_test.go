package memory

import (
	"context"
	"fmt"
	"time"
)

// sighting observes topics as if in the n-th separate conversation, on the
// n-th day. Recurrence is measured across conversations and days, so a test
// that wants a subject promoted has to supply distinct ones.
func sighting(svc *Service, ctx context.Context, n int, topics []string) []string {
	base := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)
	svc.now = func() time.Time { return base.AddDate(0, 0, n) }
	return svc.observeQuestionTopicsIn(ctx, fmt.Sprintf("session-%d", n), topics)
}
