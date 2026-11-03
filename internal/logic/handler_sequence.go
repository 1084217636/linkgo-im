package logic

import (
	"context"
	"fmt"

	"github.com/redis/go-redis/v9"
)

var sessionSeqScript = redis.NewScript(`
local nextSeq = redis.call("INCR", KEYS[1])
redis.call("PEXPIRE", KEYS[1], ARGV[1])
return nextSeq
`)

func (h *LogicHandler) nextSequence(ctx context.Context, sessionID string) (int64, error) {
	if h.Rdb == nil {
		return 0, fmt.Errorf("redis is required for sequence allocation")
	}
	key := "seq:" + sessionID
	if err := h.ensureSequenceInitialized(ctx, key, sessionID); err != nil {
		return 0, err
	}
	return sessionSeqScript.Run(ctx, h.Rdb, []string{key}, sequenceTTL.Milliseconds()).Int64()
}

func (h *LogicHandler) ensureSequenceInitialized(ctx context.Context, key, sessionID string) error {
	if h.DB == nil {
		return nil
	}
	exists, err := h.Rdb.Exists(ctx, key).Result()
	if err != nil {
		return err
	}
	if exists > 0 {
		return nil
	}

	var maxSeq int64
	if err := h.DB.QueryRowContext(ctx, `
SELECT COALESCE(MAX(seq), 0)
FROM messages
WHERE session_id = ?
`, sessionID).Scan(&maxSeq); err != nil {
		return err
	}
	if maxSeq <= 0 {
		return nil
	}
	return h.Rdb.SetNX(ctx, key, maxSeq, sequenceTTL).Err()
}
