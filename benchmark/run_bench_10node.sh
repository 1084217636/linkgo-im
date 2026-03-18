#!/bin/bash
set -euo pipefail

BASE=$(dirname "$0")
LOGDIR="$BASE/logs"
mkdir -p "$LOGDIR"

COMPOSE_FILE="$BASE/../docker-compose.10node.yml"

echo "=== 1) ensure 10-node docker-compose exists ==="
cd /home/root1/go_learn/linkgo-im

if [ ! -f "$COMPOSE_FILE" ]; then
  echo "generate docker-compose.10node.yml"
  cp docker-compose.yml docker-compose.10node.yml
  python3 - <<'PY'
from pathlib import Path
import yaml
fn='docker-compose.10node.yml'
text=Path(fn).read_text()
cfg=yaml.safe_load(text)
# remove old gateway-* if any
for k in list(cfg['services'].keys()):
    if k.startswith('gateway-'):
        cfg['services'].pop(k)
# add 10 gateways a..j
for i in range(10):
    name='gateway-' + chr(ord('a')+i)
    cfg['services'][name]={
      'build':'.',
      'container_name':'linkgo-'+name,
      'command':'./gateway',
      'ports':[f'{8090+i}:8090'],
      'depends_on':['logic','redis'],
      'environment':['GATEWAY_PORT=8090','LOGIC_ADDR=logic:9001','REDIS_ADDR=redis:6379']
    }
Path(fn).write_text(yaml.dump(cfg, sort_keys=False))
print('wrote',fn)
PY
fi

echo "=== 2) start 10-node stack ==="
cd /home/root1/go_learn/linkgo-im
docker-compose -f docker-compose.10node.yml down --remove-orphans
docker-compose -f docker-compose.10node.yml up --build -d
sleep 12

docker-compose -f docker-compose.10node.yml ps

echo "=== 3) quick availability check ==="
python3 - <<'PY'
import urllib.request
for p in range(8090, 8100):
    u=f'http://127.0.0.1:{p}/api/v1/history?user_id=1&target_id=2'
    try:
        r=urllib.request.urlopen(u, timeout=5)
        print(p, r.status)
    except Exception as e:
        print(p, 'ERROR', e)
PY

echo "=== 4) run 10k concurrent tests (1000 per gateway) ==="
if ! command -v hey >/dev/null 2>&1; then
  echo "install hey..."
  go install github.com/rakyll/hey@latest
fi

# 发起并行请求
for p in $(seq 8090 8099); do
  echo "benchmarking 127.0.0.1:$p"
  ~/go/bin/hey -z 20s -c 1000 -q 20 -m GET "http://127.0.0.1:$p/api/v1/history?user_id=1&target_id=2" > "$LOGDIR/hey_${p}.log" 2>&1 &
  sleep 0.2
done
wait

echo "=== 5) run WebSocket 10k connections test ==="
cat > /tmp/ws10k.go <<'EOF'
package main
import (
  "fmt"
  "net/url"
  "sync"
  "time"
  "github.com/gorilla/websocket"
)

func main() {
  totalGateways := 10
  perGateway := 1000  // 1000*10 = 10000 ws
  timeout := 30 * time.Second

  var wg sync.WaitGroup
  var mu sync.Mutex
  success := 0
  failed := 0

  for i := 0; i < totalGateways; i++ {
    port := 8090 + i
    for j := 0; j < perGateway; j++ {
      wg.Add(1)
      go func(port, idx int) {
        defer wg.Done()
        u := url.URL{Scheme: "ws", Host: fmt.Sprintf("127.0.0.1:%d", port), Path: "/ws", RawQuery: fmt.Sprintf("user_id=g%d_u%d", port, idx)}
        conn, _, err := websocket.DefaultDialer.Dial(u.String(), nil)
        if err != nil {
          mu.Lock(); failed++; mu.Unlock();
          return
        }
        defer conn.Close()

        mu.Lock(); success++; mu.Unlock()

        // 10s 内每2秒发一次 PING
        ticker := time.NewTicker(2 * time.Second)
        defer ticker.Stop()
        end := time.After(timeout)
        for {
          select {
          case <-end:
            return
          case <-ticker.C:
            if err := conn.WriteMessage(websocket.TextMessage, []byte("PING")); err != nil {
              return
            }
            conn.SetReadDeadline(time.Now().Add(5 * time.Second))
            _, _, err := conn.ReadMessage()
            if err != nil {
              return
            }
          }
        }
      }(port, j)
      if (j+1)%200 == 0 {
        time.Sleep(200 * time.Millisecond)
      }
    }
  }

  wg.Wait()
  fmt.Printf("ws total created=%d success=%d failed=%d\n", totalGateways*perGateway, success, failed)
}
EOF

# run ws test
if ! go list github.com/gorilla/websocket >/dev/null 2>&1; then
  printf "\ninstalling gorilla/websocket...\n"
  go install github.com/gorilla/websocket@latest
fi

# use go run to execute
go run /tmp/ws10k.go > "$LOGDIR/ws_10node.log" 2>&1 || true
cat "$LOGDIR/ws_10node.log"


echo "=== 6) aggregate results ==="
printf "Port QPS Success failed\n" > "$LOGDIR/10node_summary.log"
for p in $(seq 8090 8099); do
  qps=$(grep 'Requests/sec' "$LOGDIR/hey_${p}.log" | awk '{print $2}')
  succ=$(grep -E '\[200\]' -n "$LOGDIR/hey_${p}.log" | awk '{print $2}')
  fail=$(grep -E '\[5..\]' "$LOGDIR/hey_${p}.log" | awk '{sum += $2} END{print sum+0}')
  echo "$p $qps $succ $fail" >> "$LOGDIR/10node_summary.log"
done
cat "$LOGDIR/10node_summary.log"

echo "=== 6) platform metrics ==="
docker stats --no-stream --format 'table {{.Name}}\t{{.CPUPerc}}\t{{.MemUsage}}' > "$LOGDIR/docker_stats_10node.log"
cat "$LOGDIR/docker_stats_10node.log"

echo "=== 7) gather logs ==="
docker-compose -f docker-compose.10node.yml logs --tail 50 > "$LOGDIR/service_10node.log"

echo "=== done ==="
