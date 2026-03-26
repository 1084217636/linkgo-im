package server

import (
	"context"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

const routeSeparator = "|"

var clearRouteScript = redis.NewScript(`
if redis.call("GET", KEYS[1]) == ARGV[1] then
  return redis.call("DEL", KEYS[1])
end
return 0
`)

func BuildRouteValue(gatewayID, sessionID string) string {
	return gatewayID + routeSeparator + sessionID
}

func ParseGatewayID(routeValue string) string {
	if idx := strings.Index(routeValue, routeSeparator); idx >= 0 {
		return routeValue[:idx]
	}
	return routeValue
}

func RefreshRoute(ctx context.Context, rdb *redis.Client, uid, routeValue string, ttl time.Duration) error {
	return rdb.Set(ctx, "route:"+uid, routeValue, ttl).Err()
}

func ClearRouteIfMatch(ctx context.Context, rdb *redis.Client, uid, routeValue string) error {
	return clearRouteScript.Run(ctx, rdb, []string{"route:" + uid}, routeValue).Err()
}
