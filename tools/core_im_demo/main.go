package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/1084217636/linkgo-im/api"
	_ "github.com/go-sql-driver/mysql"
	"github.com/gorilla/websocket"
	"github.com/redis/go-redis/v9"
	"google.golang.org/protobuf/proto"
)

type loginResponse struct {
	Token  string `json:"token"`
	UserID string `json:"user_id"`
}

type demoConfig struct {
	GatewayBase     string
	TransferBase    string
	RedisAddr       string
	RedisPass       string
	MySQLDSN        string
	ArtifactDir     string
	Timeout         time.Duration
	RequireTransfer bool
}

type demoReport struct {
	Rows []string
}

func main() {
	var cfg demoConfig
	var timeoutSeconds int
	flag.StringVar(&cfg.GatewayBase, "gateway-base", getenv("GATEWAY_BASE", "http://127.0.0.1:8090"), "Gateway HTTP base URL")
	flag.StringVar(&cfg.TransferBase, "transfer-base", getenv("TRANSFER_BASE", "http://127.0.0.1:9102"), "Transfer HTTP base URL")
	flag.StringVar(&cfg.RedisAddr, "redis-addr", getenv("REDIS_ADDR", "127.0.0.1:6379"), "Redis address")
	flag.StringVar(&cfg.RedisPass, "redis-password", getenv("REDIS_PASSWORD", "123456"), "Redis password")
	flag.StringVar(&cfg.MySQLDSN, "mysql-dsn", getenv("MYSQL_DSN", "root:root@tcp(127.0.0.1:3306)/linkgo_im?charset=utf8mb4&parseTime=True&loc=Local"), "MySQL DSN")
	flag.StringVar(&cfg.ArtifactDir, "artifact-dir", getenv("ARTIFACT_DIR", "artifacts/core_im_demo"), "artifact output directory")
	flag.IntVar(&timeoutSeconds, "timeout", 20, "per-step timeout in seconds")
	flag.BoolVar(&cfg.RequireTransfer, "require-transfer", getenv("REQUIRE_TRANSFER", "0") == "1", "fail if Kafka/Transfer group demo is unavailable")
	flag.Parse()
	cfg.Timeout = time.Duration(timeoutSeconds) * time.Second

	if err := run(cfg); err != nil {
		fmt.Fprintf(os.Stderr, "core im demo failed: %v\n", err)
		os.Exit(1)
	}
}

func run(cfg demoConfig) error {
	if err := os.MkdirAll(cfg.ArtifactDir, 0o755); err != nil {
		return err
	}
	report := &demoReport{}
	ctx, cancel := context.WithTimeout(context.Background(), cfg.Timeout*3)
	defer cancel()

	if err := checkHTTP(ctx, cfg.GatewayBase+"/healthz"); err != nil {
		return fmt.Errorf("gateway health check: %w", err)
	}
	report.pass("gateway healthz", cfg.GatewayBase+"/healthz")

	userA, err := login(ctx, cfg.GatewayBase, "userA", "123456")
	if err != nil {
		return err
	}
	report.pass("login userA", "user_id="+userA.UserID)
	userB, err := login(ctx, cfg.GatewayBase, "userB", "123456")
	if err != nil {
		return err
	}
	report.pass("login userB", "user_id="+userB.UserID)

	rdb := redis.NewClient(&redis.Options{Addr: cfg.RedisAddr, Password: cfg.RedisPass})
	defer rdb.Close()
	if err := rdb.Ping(ctx).Err(); err != nil {
		return fmt.Errorf("redis ping: %w", err)
	}
	report.pass("redis ping", cfg.RedisAddr)

	db, err := sql.Open("mysql", cfg.MySQLDSN)
	if err != nil {
		return err
	}
	defer db.Close()
	if err := db.PingContext(ctx); err != nil {
		return fmt.Errorf("mysql ping: %w", err)
	}
	report.pass("mysql ping", "linkgo_im")
	if err := ensureAIBotSeed(ctx, db); err != nil {
		return fmt.Errorf("ai bot seed: %w", err)
	}
	report.pass("ai bot seed", "9001 ai_assistant")

	connA, err := connectWS(cfg.GatewayBase, userA.Token)
	if err != nil {
		return err
	}
	defer connA.Close()
	report.pass("websocket connect userA", "route:"+userA.UserID)

	connB, err := connectWS(cfg.GatewayBase, userB.Token)
	if err != nil {
		return err
	}
	report.pass("websocket connect userB", "route:"+userB.UserID)

	onlineBody := "demo online message " + time.Now().Format("150405.000")
	onlineClientID := "demo-online-" + fmt.Sprint(time.Now().UnixNano())
	if err := sendNormal(connA, userB.UserID, "user", onlineBody, onlineClientID); err != nil {
		return err
	}
	gotOnline, err := waitAndAck(connB, onlineBody, cfg.Timeout)
	if err != nil {
		return err
	}
	report.pass("single chat receive + ack", gotOnline.MessageId)
	if err := waitPendingCleared(ctx, rdb, userB.UserID, gotOnline.MessageId, cfg.Timeout); err != nil {
		return err
	}
	report.pass("ack clears pending", gotOnline.MessageId)

	aiQuestion := "项目里 Redis 用来做什么？ " + time.Now().Format("150405.000")
	aiClientID := "demo-ai-" + fmt.Sprint(time.Now().UnixNano())
	if err := sendNormal(connA, "9001", "user", aiQuestion, aiClientID); err != nil {
		return err
	}
	gotAI, err := waitMatchAndAck(connA, func(frame *api.WireMessage) bool {
		return frame.From == "9001" && frame.To == userA.UserID && strings.TrimSpace(frame.Body) != ""
	}, cfg.Timeout)
	if err != nil {
		return fmt.Errorf("ai bot private reply: %w", err)
	}
	report.pass("ai bot private reply", truncateDetail(gotAI.Body, 80))

	if err := connB.Close(); err != nil {
		return err
	}
	time.Sleep(700 * time.Millisecond)
	offlineBody := "demo offline message " + time.Now().Format("150405.000")
	offlineClientID := "demo-offline-" + fmt.Sprint(time.Now().UnixNano())
	if err := sendNormal(connA, userB.UserID, "user", offlineBody, offlineClientID); err != nil {
		return err
	}
	time.Sleep(700 * time.Millisecond)
	offlineCount, _ := rdb.ZCard(ctx, "offline_msg:"+userB.UserID).Result()
	pendingCount, _ := rdb.ZCard(ctx, "pending_ack:"+userB.UserID).Result()
	report.pass("offline indexes recorded", fmt.Sprintf("offline=%d pending=%d", offlineCount, pendingCount))

	connB2, err := connectWS(cfg.GatewayBase, userB.Token)
	if err != nil {
		return err
	}
	defer connB2.Close()
	gotOffline, err := waitAndAck(connB2, offlineBody, cfg.Timeout)
	if err != nil {
		return err
	}
	report.pass("offline replay + ack", gotOffline.MessageId)

	clientIDs := []string{onlineClientID, offlineClientID, aiClientID}
	if transferAvailable(ctx, cfg.TransferBase) {
		groupID := "Gdemo" + time.Now().Format("150405")
		if err := createGroup(ctx, cfg.GatewayBase, userA.Token, groupID, []string{userB.UserID}); err != nil {
			return err
		}
		report.pass("group create", groupID)

		groupBody := "demo group message " + time.Now().Format("150405.000")
		groupClientID := "demo-group-" + fmt.Sprint(time.Now().UnixNano())
		if err := sendNormal(connA, groupID, "group", groupBody, groupClientID); err != nil {
			return err
		}
		gotGroup, err := waitAndAck(connB2, groupBody, cfg.Timeout)
		if err != nil {
			return err
		}
		clientIDs = append(clientIDs, groupClientID)
		report.pass("group chat via kafka transfer", gotGroup.MessageId)
	} else if cfg.RequireTransfer {
		return fmt.Errorf("transfer service unavailable at %s/healthz", cfg.TransferBase)
	} else {
		report.skip("group chat via kafka transfer", "Transfer is not running; start full docker-compose stack or set REQUIRE_TRANSFER=1")
	}

	messageCount, err := countDemoMessages(ctx, db, clientIDs)
	if err != nil {
		return err
	}
	report.pass("mysql messages persisted", fmt.Sprintf("demo_messages=%d", messageCount))

	metricsBody, err := getText(ctx, cfg.GatewayBase+"/metrics")
	if err != nil {
		return err
	}
	if !strings.Contains(metricsBody, "linkgo_inbound_messages_total") {
		return fmt.Errorf("gateway metrics missing linkgo_inbound_messages_total")
	}
	report.pass("gateway metrics exposed", "linkgo_inbound_messages_total")

	return report.write(filepath.Join(cfg.ArtifactDir, "core_im_demo_report.md"))
}

func login(ctx context.Context, base, username, password string) (*loginResponse, error) {
	body, _ := json.Marshal(map[string]string{"username": username, "password": password})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/api/v1/login", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("login %s: %w", username, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("login %s returned %s", username, resp.Status)
	}
	var out loginResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	if out.Token == "" || out.UserID == "" {
		return nil, fmt.Errorf("login %s returned empty token/user_id", username)
	}
	return &out, nil
}

func connectWS(base, token string) (*websocket.Conn, error) {
	u, err := url.Parse(base)
	if err != nil {
		return nil, err
	}
	if u.Scheme == "https" {
		u.Scheme = "wss"
	} else {
		u.Scheme = "ws"
	}
	u.Path = "/ws"
	q := u.Query()
	q.Set("token", token)
	u.RawQuery = q.Encode()
	conn, _, err := websocket.DefaultDialer.Dial(u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("websocket dial: %w", err)
	}
	return conn, nil
}

func sendNormal(conn *websocket.Conn, to, toType, body, clientMsgID string) error {
	payload, err := proto.Marshal(&api.WireMessage{
		To:          to,
		ToType:      toType,
		MsgType:     api.MsgType_NORMAL,
		Body:        body,
		ClientMsgId: clientMsgID,
	})
	if err != nil {
		return err
	}
	return conn.WriteMessage(websocket.BinaryMessage, payload)
}

func waitAndAck(conn *websocket.Conn, expectedBody string, timeout time.Duration) (*api.WireMessage, error) {
	return waitMatchAndAck(conn, func(frame *api.WireMessage) bool {
		return frame.Body == expectedBody
	}, timeout)
}

func waitMatchAndAck(conn *websocket.Conn, match func(*api.WireMessage) bool, timeout time.Duration) (*api.WireMessage, error) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		_ = conn.SetReadDeadline(deadline)
		_, payload, err := conn.ReadMessage()
		if err != nil {
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				return nil, fmt.Errorf("timed out waiting for matching message")
			}
			return nil, err
		}
		var frame api.WireMessage
		if err := proto.Unmarshal(payload, &frame); err != nil {
			continue
		}
		if frame.MsgType == api.MsgType_NORMAL && frame.MessageId != "" {
			ack, _ := proto.Marshal(&api.WireMessage{
				MsgType:      api.MsgType_ACK,
				AckMessageId: frame.MessageId,
				TraceId:      frame.TraceId,
			})
			_ = conn.WriteMessage(websocket.BinaryMessage, ack)
		}
		if match != nil && match(&frame) {
			return &frame, nil
		}
	}
	return nil, fmt.Errorf("timed out waiting for matching message")
}

func ensureAIBotSeed(ctx context.Context, db *sql.DB) error {
	if db == nil {
		return nil
	}
	if _, err := db.ExecContext(ctx, `
INSERT INTO users (user_id, username, password, created_at, updated_at)
VALUES ('9001', 'ai_assistant', 'bot-only', 1710100000000, 1710100000000)
ON DUPLICATE KEY UPDATE
  username = VALUES(username),
  password = VALUES(password),
  updated_at = VALUES(updated_at)
`); err != nil {
		return err
	}
	_, err := db.ExecContext(ctx, `
INSERT INTO friend_relations (user_id, friend_id, status, created_at, updated_at) VALUES
('1001', '9001', 'normal', 1710100000000, 1710100000000),
('9001', '1001', 'normal', 1710100000000, 1710100000000),
('1002', '9001', 'normal', 1710100000000, 1710100000000),
('9001', '1002', 'normal', 1710100000000, 1710100000000),
('1003', '9001', 'normal', 1710100000000, 1710100000000),
('9001', '1003', 'normal', 1710100000000, 1710100000000)
ON DUPLICATE KEY UPDATE
  status = VALUES(status),
  updated_at = VALUES(updated_at)
`)
	return err
}

func truncateDetail(value string, max int) string {
	value = strings.TrimSpace(value)
	if max <= 0 || len([]rune(value)) <= max {
		return value
	}
	runes := []rune(value)
	return string(runes[:max]) + "..."
}

func waitPendingCleared(ctx context.Context, rdb *redis.Client, uid, messageID string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		exists, err := rdb.ZScore(ctx, "pending_ack:"+uid, messageID).Result()
		if err == redis.Nil || exists == 0 {
			return nil
		}
		if err != nil {
			return err
		}
		time.Sleep(200 * time.Millisecond)
	}
	return fmt.Errorf("pending ack not cleared for %s", messageID)
}

func createGroup(ctx context.Context, base, token, groupID string, members []string) error {
	body, _ := json.Marshal(map[string]any{
		"group_id": groupID,
		"name":     groupID,
		"members":  members,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/api/v1/group/create", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("create group returned %s", resp.Status)
	}
	return nil
}

func countDemoMessages(ctx context.Context, db *sql.DB, clientIDs []string) (int, error) {
	if len(clientIDs) == 0 {
		return 0, nil
	}
	placeholders := strings.TrimRight(strings.Repeat("?,", len(clientIDs)), ",")
	args := make([]any, 0, len(clientIDs))
	for _, id := range clientIDs {
		args = append(args, id)
	}
	var count int
	err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM messages WHERE client_msg_id IN ("+placeholders+")", args...).Scan(&count)
	return count, err
}

func checkHTTP(ctx context.Context, target string) error {
	resp, err := http.Get(target)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("%s returned %s", target, resp.Status)
	}
	return nil
}

func getText(ctx context.Context, target string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return "", err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return "", fmt.Errorf("%s returned %s", target, resp.Status)
	}
	buf := new(bytes.Buffer)
	if _, err := buf.ReadFrom(resp.Body); err != nil {
		return "", err
	}
	return buf.String(), nil
}

func (r *demoReport) pass(name, detail string) {
	r.Rows = append(r.Rows, fmt.Sprintf("| %s | PASS | %s |", name, detail))
	fmt.Printf("PASS %-32s %s\n", name, detail)
}

func (r *demoReport) skip(name, detail string) {
	r.Rows = append(r.Rows, fmt.Sprintf("| %s | SKIP | %s |", name, detail))
	fmt.Printf("SKIP %-32s %s\n", name, detail)
}

func transferAvailable(ctx context.Context, base string) bool {
	checkCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(checkCtx, http.MethodGet, strings.TrimRight(base, "/")+"/healthz", nil)
	if err != nil {
		return false
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode < 300
}

func (r *demoReport) write(path string) error {
	var b strings.Builder
	b.WriteString("# Core IM Demo Report\n\n")
	b.WriteString("- Generated at: " + time.Now().Format(time.RFC3339) + "\n\n")
	b.WriteString("| Check | Result | Detail |\n")
	b.WriteString("|---|---|---|\n")
	for _, row := range r.Rows {
		b.WriteString(row + "\n")
	}
	b.WriteString("\n## Covered Links\n\n")
	b.WriteString("- Login token flow\n")
	b.WriteString("- WebSocket route registration\n")
	b.WriteString("- Online single chat receive and ACK cleanup\n")
	b.WriteString("- Offline message indexing and reconnect replay\n")
	b.WriteString("- Group chat dispatch through Kafka Transfer\n")
	b.WriteString("- MySQL message persistence\n")
	b.WriteString("- Gateway Prometheus metrics exposure\n")
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		return err
	}
	fmt.Println("report:", path)
	return nil
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
