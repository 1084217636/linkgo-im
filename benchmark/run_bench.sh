#!/bin/bash
set -euo pipefail

BASE=$(dirname "$0")
LOGDIR="$BASE/logs"
mkdir -p "$LOGDIR"

echo "=== 1) start services ==="
cd /home/root1/go_learn/linkgo-im
docker-compose down --remove-orphans
docker-compose up --build -d
sleep 8

docker-compose ps

echo "=== 2) quick availability checks ==="
python3 - <<'PY'
import urllib.request
for u in ['http://127.0.0.1:8090/api/v1/history?user_id=1&target_id=2', 'http://127.0.0.1:8091/api/v1/history?user_id=1&target_id=2']:
    try:
        r=urllib.request.urlopen(u, timeout=5)
        print(f'{u} status={r.status} body={r.read(256)}')
    except Exception as e:
        print(f'{u} error={e}')
PY

echo "=== 3) hey HTTP pressure tests ==="
if ! command -v hey >/dev/null 2>&1; then
  echo "install hey..."
  go install github.com/rakyll/hey@latest
fi
for c in 100 300 500; do
  echo "[hey c=$c]"
  ~/go/bin/hey -z 20s -c $c -q 20 -m GET 'http://127.0.0.1:8090/api/v1/history?user_id=1&target_id=2' > "$LOGDIR/hey_$c.log" 2>&1 || true
  tail -n 30 "$LOGDIR/hey_$c.log"
  echo "---"
done

echo "=== 4) WebSocket 300并发压力测试 ==="
cat > /tmp/ws_bench.go <<'EOF'
package main
import (
  "fmt"
  "sync"
  "time"
  "net/url"
  "github.com/gorilla/websocket"
)
func main() {
  const concurrency = 300
  const duration = 30 * time.Second
  var wg sync.WaitGroup
  success := 0
  failed := 0
  var mu sync.Mutex
  for i := 0; i < concurrency; i++ {
    wg.Add(1)
    go func(i int) {
      defer wg.Done()
      u := url.URL{Scheme: "ws", Host: "127.0.0.1:8090", Path: "/ws", RawQuery: fmt.Sprintf("user_id=uid%%d", i)}
      c, _, err := websocket.DefaultDialer.Dial(u.String(), nil)
      if err != nil {
        mu.Lock(); failed++; mu.Unlock();
        return
      }
      defer c.Close()
      mu.Lock(); success++; mu.Unlock()
      ticker := time.NewTicker(3 * time.Second)
      defer ticker.Stop()
      timeout := time.After(duration)
      for {
        select {
        case <-timeout:
          return
        case <-ticker.C:
          if err := c.WriteMessage(websocket.TextMessage, []byte("PING")); err != nil {
            return
          }
          _ = c.SetReadDeadline(time.Now().Add(5 * time.Second))
          _, _, err := c.ReadMessage()
          if err != nil {
            return
          }
        }
      }
    }(i)
  }
  wg.Wait()
  fmt.Printf("done success=%d failed=%d\n", success, failed)
}
EOF

go run /tmp/ws_bench.go > "$LOGDIR/ws_bench.log" 2>&1
cat "$LOGDIR/ws_bench.log"

echo "=== 5) platform metrics ==="
docker stats --no-stream --format 'table {{.Name}}\t{{.CPUPerc}}\t{{.MemUsage}}' > "$LOGDIR/docker_stats.log"
cat "$LOGDIR/docker_stats.log"

echo "=== 6) ingest logs ==="
docker-compose logs --tail 40 gateway-a gateway-b logic > "$LOGDIR/service.log"
cat "$LOGDIR/service.log"

echo "=== done ==="
