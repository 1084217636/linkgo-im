package server

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/zeromicro/go-zero/core/logx"
)

const routeSeparator = "|"
const gatewayRouteTTLMultiplier = 2

var clearRouteScript = redis.NewScript(`
if redis.call("GET", KEYS[1]) == ARGV[1] then
  redis.call("DEL", KEYS[1])
  redis.call("SREM", KEYS[2], ARGV[2])
  if ARGV[3] ~= "" then
    redis.call("DEL", KEYS[3])
  end
  return 1
end
return 0
`)

func BuildRouteValue(gatewayID, sessionID string) string {
	return gatewayID + routeSeparator + sessionID
}

func ParseRoute(routeValue string) (gatewayID, sessionID string) {
	if idx := strings.Index(routeValue, routeSeparator); idx >= 0 {
		return routeValue[:idx], routeValue[idx+len(routeSeparator):]
	}
	return routeValue, ""
}

func ParseGatewayID(routeValue string) string {
	gatewayID, _ := ParseRoute(routeValue)
	return gatewayID
}

func RefreshRoute(ctx context.Context, rdb *redis.Client, uid, routeValue string, ttl time.Duration) error {
	gatewayID, sessionID := ParseRoute(routeValue)
	if gatewayID == "" {
		return rdb.Set(ctx, RouteKey(uid), routeValue, ttl).Err()
	}

	reverseTTL := ttl * gatewayRouteTTLMultiplier
	pipe := rdb.TxPipeline()
	pipe.Set(ctx, RouteKey(uid), routeValue, ttl)
	pipe.SAdd(ctx, GatewayUsersKey(gatewayID), uid)
	pipe.Expire(ctx, GatewayUsersKey(gatewayID), reverseTTL)
	if sessionID != "" {
		pipe.Set(ctx, GatewayConnKey(gatewayID, sessionID), uid, ttl)
	}
	pipe.Set(ctx, GatewayLiveKey(gatewayID), time.Now().UnixMilli(), ttl)
	_, err := pipe.Exec(ctx)
	return err
}

func ClearRouteIfMatch(ctx context.Context, rdb *redis.Client, uid, routeValue string) error {
	gatewayID, sessionID := ParseRoute(routeValue)
	return clearRouteScript.Run(ctx, rdb, []string{
		RouteKey(uid),
		GatewayUsersKey(gatewayID),
		GatewayConnKey(gatewayID, sessionID),
	}, routeValue, uid, sessionID).Err()
}

func CleanupGatewayRoutes(ctx context.Context, rdb *redis.Client, gatewayID string) (int64, error) {
	if gatewayID == "" {
		return 0, nil
	}

	usersKey := GatewayUsersKey(gatewayID)
	users, err := rdb.SMembers(ctx, usersKey).Result()
	if err != nil && err != redis.Nil {
		return 0, err
	}

	var cleaned int64
	for _, uid := range users {
		routeValue, err := rdb.Get(ctx, RouteKey(uid)).Result()
		if err == redis.Nil {
			_ = rdb.SRem(ctx, usersKey, uid).Err()
			continue
		}
		if err != nil {
			return cleaned, err
		}
		if ParseGatewayID(routeValue) != gatewayID {
			_ = rdb.SRem(ctx, usersKey, uid).Err()
			continue
		}
		if err := ClearRouteIfMatch(ctx, rdb, uid, routeValue); err != nil {
			return cleaned, err
		}
		cleaned++
	}

	var cursor uint64
	for {
		keys, next, err := rdb.Scan(ctx, cursor, GatewayConnKey(gatewayID, "*"), 100).Result()
		if err != nil {
			return cleaned, err
		}
		if len(keys) > 0 {
			if err := rdb.Del(ctx, keys...).Err(); err != nil {
				return cleaned, err
			}
		}
		if next == 0 {
			break
		}
		cursor = next
	}
	_ = rdb.Del(ctx, GatewayLiveKey(gatewayID)).Err()
	return cleaned, nil
}

func StartGatewayHeartbeat(ctx context.Context, rdb *redis.Client, gatewayID string, ttl time.Duration) {
	if gatewayID == "" || ttl <= 0 {
		return
	}
	interval := ttl / 3
	if interval < time.Second {
		interval = time.Second
	}

	pulse := func() {
		if err := rdb.Set(ctx, GatewayLiveKey(gatewayID), time.Now().UnixMilli(), ttl).Err(); err != nil {
			logx.Errorw("gateway heartbeat refresh failed",
				logx.Field("gateway_id", gatewayID),
				logx.Field("error", err.Error()),
			)
			return
		}
		_ = rdb.Expire(ctx, GatewayUsersKey(gatewayID), ttl*gatewayRouteTTLMultiplier).Err()
	}

	pulse()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			pulse()
		}
	}
}

func RouteKey(uid string) string {
	return "route:" + uid
}

func GatewayUsersKey(gatewayID string) string {
	return "gateway_users:" + gatewayID
}

func GatewayConnKey(gatewayID, sessionID string) string {
	return fmt.Sprintf("gateway_conn:%s:%s", gatewayID, sessionID)
}

func GatewayLiveKey(gatewayID string) string {
	return "gateway_live:" + gatewayID
}

func PendingAckKey(uid string) string {
	return "pending_ack:" + uid
}

func AckIndexKey(uid string) string {
	return "ack_idx:" + uid
}

func AckRetryKey(uid string) string {
	return "ack_retry:" + uid
}

func OfflineMessageKey(uid string) string {
	return "offline_msg:" + uid
}

func SessionTimelineKey(sessionID string) string {
	return "session_timeline:" + sessionID
}

func MessagePayloadKey(messageID string) string {
	return "message_payload:" + messageID
}

func UserConversationsKey(uid string) string {
	return "user:conversations:" + uid
}

func ConversationMembersKey(conversationID string) string {
	return "conversation:members:" + conversationID
}

func ConversationLastKey(conversationID string) string {
	return "conversation:last:" + conversationID
}

func UserConversationReadKey(uid string) string {
	return "user:conversation:read:" + uid
}
