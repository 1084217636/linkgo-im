package main

import (
	"context"
	"errors"
	"time"

	"github.com/redis/go-redis/v9"
)

type recipientClaimResult string

const (
	recipientClaimed recipientClaimResult = "claimed"
	recipientBusy    recipientClaimResult = "busy"
	recipientDone    recipientClaimResult = "done"

	recipientProcessingLease = time.Minute
	recipientDoneTTL         = 7 * 24 * time.Hour
)

var errRecipientLeaseBusy = errors.New("recipient delivery lease is held by another worker")

var claimGroupRecipientScript = redis.NewScript(`
local current = redis.call("GET", KEYS[1])
if not current then
  redis.call("SET", KEYS[1], "processing:" .. ARGV[1], "PX", ARGV[2], "NX")
  return "claimed"
end
if current == "done" then
  return "done"
end
if current == "processing:" .. ARGV[1] then
  redis.call("PEXPIRE", KEYS[1], ARGV[2])
  return "claimed"
end
return "busy"
`)

var completeGroupRecipientScript = redis.NewScript(`
if redis.call("GET", KEYS[1]) ~= "processing:" .. ARGV[1] then
  return 0
end
redis.call("SET", KEYS[1], "done", "PX", ARGV[2])
return 1
`)

var releaseGroupRecipientScript = redis.NewScript(`
if redis.call("GET", KEYS[1]) == "processing:" .. ARGV[1] then
  return redis.call("DEL", KEYS[1])
end
return 0
`)

func claimGroupRecipient(
	ctx context.Context,
	rdb *redis.Client,
	key string,
	owner string,
	lease time.Duration,
) (recipientClaimResult, error) {
	result, err := claimGroupRecipientScript.Run(ctx, rdb, []string{key}, owner, lease.Milliseconds()).Text()
	return recipientClaimResult(result), err
}

func completeGroupRecipient(
	ctx context.Context,
	rdb *redis.Client,
	key string,
	owner string,
	doneTTL time.Duration,
) (bool, error) {
	result, err := completeGroupRecipientScript.Run(ctx, rdb, []string{key}, owner, doneTTL.Milliseconds()).Int64()
	return result == 1, err
}

func releaseGroupRecipient(ctx context.Context, rdb *redis.Client, key, owner string) error {
	return releaseGroupRecipientScript.Run(ctx, rdb, []string{key}, owner).Err()
}
