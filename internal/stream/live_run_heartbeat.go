package stream

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// LiveRunHeartbeatTTL is how long a live run counts as alive after its last
// heartbeat. The process running the turn renews it (see
// LiveRunHeartbeatInterval); when that process dies — a restart, a crash, a
// deploy — the heartbeat lapses even though readers keep extending the
// live-run marker itself, so a new turn can tell the run is gone.
const (
	LiveRunHeartbeatTTL      = 60 * time.Second
	LiveRunHeartbeatInterval = 20 * time.Second
)

// heartbeatLiveRunCAS renews the heartbeat only while the marker still names
// the run; otherwise it drops the heartbeat and reports 0 so the runner stops.
var heartbeatLiveRunCAS = redis.NewScript(`
local raw = redis.call('GET', KEYS[1])
if not raw or string.find(raw, ARGV[1], 1, true) == nil then
  redis.call('DEL', KEYS[2])
  return 0
end
redis.call('SET', KEYS[2], ARGV[1], 'EX', ARGV[2])
return 1
`)

func (r *RedisStreamManager) buildLiveRunHeartbeatKey(sessionID string) string {
	return fmt.Sprintf("%s:%s:live-run:hb", r.prefix, sessionID)
}

// HeartbeatLiveRun renews the heartbeat of the session's live run. It
// returns false once the marker names another run or none.
func (r *RedisStreamManager) HeartbeatLiveRun(ctx context.Context, sessionID, assistantMessageID string) (bool, error) {
	if sessionID == "" || assistantMessageID == "" {
		return false, nil
	}
	n, err := heartbeatLiveRunCAS.Run(ctx, r.client,
		[]string{r.buildLiveRunKey(sessionID), r.buildLiveRunHeartbeatKey(sessionID)},
		assistantMessageID, int(LiveRunHeartbeatTTL/time.Second),
	).Int()
	if err != nil {
		return false, fmt.Errorf("failed to renew live run heartbeat: %w", err)
	}
	return n == 1, nil
}

// LiveRunAlive reports whether the run's heartbeat is current.
func (r *RedisStreamManager) LiveRunAlive(ctx context.Context, sessionID, assistantMessageID string) (bool, error) {
	v, err := r.client.Get(ctx, r.buildLiveRunHeartbeatKey(sessionID)).Result()
	if err == redis.Nil {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("failed to read live run heartbeat: %w", err)
	}
	return v == assistantMessageID, nil
}
