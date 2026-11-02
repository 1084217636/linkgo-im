// reconnect_demo seeds two real conversations and checks offline replay via
// public HTTP/WebSocket APIs. It only uses the demo users from sql/init.sql.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/1084217636/linkgo-im/api"
	"github.com/gorilla/websocket"
	"github.com/redis/go-redis/v9"
	"google.golang.org/protobuf/proto"
)

type user struct {
	Token  string `json:"token"`
	UserID string `json:"user_id"`
}
type history struct {
	Data []struct {
		Body      string `json:"body"`
		MessageID string `json:"message_id"`
		Seq       int64  `json:"seq"`
	} `json:"data"`
	HasMore       bool  `json:"has_more"`
	NextBeforeSeq int64 `json:"next_before_seq"`
}

func main() {
	base := flag.String("gateway", "http://127.0.0.1:8090", "Gateway HTTP address")
	flag.Parse()
	if err := run(*base); err != nil {
		fmt.Fprintln(os.Stderr, "FAIL:", err)
		os.Exit(1)
	}
}
func run(base string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	rdb := redis.NewClient(&redis.Options{Addr: "127.0.0.1:6379", Password: "123456"})
	defer rdb.Close()
	if err := rdb.Ping(ctx).Err(); err != nil {
		return fmt.Errorf("redis: %w", err)
	}
	a, err := login(ctx, base, "userA")
	if err != nil {
		return err
	}
	b, err := login(ctx, base, "userB")
	if err != nil {
		return err
	}
	c, err := login(ctx, base, "userC")
	if err != nil {
		return err
	}
	sender, err := dial(base, a.Token, "", -1)
	if err != nil {
		return err
	}
	defer sender.Close()
	stamp := time.Now().Format("20060102-150405")
	tag := "reconnect-demo-" + stamp
	// First batch is replayed to short-lived black-box receivers and ACKed.
	for _, item := range []struct{ to, body string }{{b.UserID, "A→B 第1条 " + tag}, {b.UserID, "A→B 第2条 " + tag}, {c.UserID, "A→C 第1条 " + tag}, {c.UserID, "A→C 第2条 " + tag}} {
		if err := send(sender, item.to, item.body, tag); err != nil {
			return err
		}
	}
	if err := waitHistory(ctx, base, a.Token, b.UserID, tag, 2); err != nil {
		return err
	}
	if err := waitHistory(ctx, base, a.Token, c.UserID, tag, 2); err != nil {
		return err
	}
	for _, item := range []struct {
		name    string
		u       user
		session string
	}{{"B", b, "c2c:1001:1002"}, {"C", c, "c2c:1001:1003"}} {
		conn, err := dial(base, item.u.Token, item.session, 0)
		if err != nil {
			return err
		}
		ids, err := receiveAndAck(conn, tag, 2)
		if err != nil {
			conn.Close()
			return fmt.Errorf("%s offline replay: %w", item.name, err)
		}
		if err := waitACKs(ctx, rdb, item.u.UserID, ids); err != nil {
			conn.Close()
			return fmt.Errorf("%s ACK: %w", item.name, err)
		}
		conn.Close()
		fmt.Printf("PASS %s: 两条离线消息已回放，Redis pending 已清除\n", item.name)
	}
	// Leave one offline message in each conversation for the real HTML page.
	time.Sleep(500 * time.Millisecond)
	for _, item := range []struct{ to, body string }{{b.UserID, "A→B 待浏览器重连 " + tag}, {c.UserID, "A→C 待浏览器重连 " + tag}} {
		if err := send(sender, item.to, item.body, tag); err != nil {
			return err
		}
	}
	if err := waitHistory(ctx, base, a.Token, b.UserID, tag, 3); err != nil {
		return err
	}
	if err := waitHistory(ctx, base, a.Token, c.UserID, tag, 3); err != nil {
		return err
	}
	if err := showTwoHistoryPages(ctx, base, b.Token, a.UserID); err != nil {
		return err
	}
	fmt.Println("PASS MySQL 历史接口: A↔B 与 A↔C 各有本轮 3 条消息")
	fmt.Println("测试标记:", tag)
	fmt.Println("现在打开 http://127.0.0.1:8088 ，分别登录 userB/userC，观察待确认回放；点击与 userA 的会话查看历史。")
	return nil
}
func login(ctx context.Context, base, name string) (user, error) {
	body, _ := json.Marshal(map[string]string{"username": name, "password": "123456"})
	req, err := http.NewRequestWithContext(ctx, "POST", base+"/api/v1/login", bytes.NewReader(body))
	if err != nil {
		return user{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return user{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		data, _ := io.ReadAll(resp.Body)
		return user{}, fmt.Errorf("login %s: %s %s", name, resp.Status, data)
	}
	var u user
	if err := json.NewDecoder(resp.Body).Decode(&u); err != nil {
		return u, err
	}
	if u.Token == "" {
		return u, fmt.Errorf("login %s returned no token", name)
	}
	return u, nil
}
func dial(base, token, session string, lastSeq int64) (*websocket.Conn, error) {
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
	if session != "" {
		q.Set("session_id", session)
		q.Set("last_seq", fmt.Sprint(lastSeq))
	}
	u.RawQuery = q.Encode()
	conn, resp, err := websocket.DefaultDialer.Dial(u.String(), http.Header{"Origin": []string{"http://127.0.0.1:8088"}})
	if err != nil {
		if resp != nil {
			return nil, fmt.Errorf("ws %s: %w", resp.Status, err)
		}
		return nil, err
	}
	return conn, nil
}
func send(conn *websocket.Conn, to, body, tag string) error {
	frame := &api.WireMessage{To: to, ToType: "user", MsgType: api.MsgType_NORMAL, Body: body, ClientMsgId: tag + "-" + to + "-" + fmt.Sprint(time.Now().UnixNano())}
	payload, err := proto.Marshal(frame)
	if err != nil {
		return err
	}
	return conn.WriteMessage(websocket.BinaryMessage, payload)
}
func receiveAndAck(conn *websocket.Conn, tag string, want int) ([]string, error) {
	deadline := time.Now().Add(12 * time.Second)
	seen := map[string]bool{}
	for len(seen) < want {
		if err := conn.SetReadDeadline(deadline); err != nil {
			return nil, err
		}
		_, payload, err := conn.ReadMessage()
		if err != nil {
			return nil, err
		}
		var msg api.WireMessage
		if proto.Unmarshal(payload, &msg) != nil || msg.MsgType != api.MsgType_NORMAL || msg.MessageId == "" {
			continue
		}
		if strings.Contains(msg.Body, tag) {
			seen[msg.MessageId] = true
			fmt.Printf("  replay seq=%d id=%s body=%s\n", msg.Seq, msg.MessageId, msg.Body)
		}
		ack, _ := proto.Marshal(&api.WireMessage{MsgType: api.MsgType_ACK, AckMessageId: msg.MessageId})
		if err := conn.WriteMessage(websocket.BinaryMessage, ack); err != nil {
			return nil, err
		}
	}
	ids := make([]string, 0, len(seen))
	for id := range seen {
		ids = append(ids, id)
	}
	return ids, nil
}
func waitHistory(ctx context.Context, base, token, target, tag string, want int) error {
	deadline := time.Now().Add(12 * time.Second)
	for time.Now().Before(deadline) {
		u := base + "/api/v1/history?target_id=" + url.QueryEscape(target) + "&limit=100"
		req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+token)
		resp, err := http.DefaultClient.Do(req)
		if err == nil {
			var h history
			decodeErr := json.NewDecoder(resp.Body).Decode(&h)
			resp.Body.Close()
			if resp.StatusCode == 200 && decodeErr == nil {
				count := 0
				for _, m := range h.Data {
					if strings.Contains(m.Body, tag) {
						count++
					}
				}
				if count >= want {
					return nil
				}
			}
		}
		time.Sleep(200 * time.Millisecond)
	}
	return fmt.Errorf("history target=%s did not contain %d demo messages", target, want)
}

func showTwoHistoryPages(ctx context.Context, base, token, target string) error {
	before := int64(0)
	for page := 1; page <= 2; page++ {
		q := url.Values{"target_id": {target}, "limit": {"2"}}
		if before > 0 {
			q.Set("before_seq", fmt.Sprint(before))
		}
		req, err := http.NewRequestWithContext(ctx, "GET", base+"/api/v1/history?"+q.Encode(), nil)
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+token)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return err
		}
		var h history
		err = json.NewDecoder(resp.Body).Decode(&h)
		resp.Body.Close()
		if err != nil || resp.StatusCode != 200 {
			return fmt.Errorf("history page %d: status=%s decode=%v", page, resp.Status, err)
		}
		seqs := make([]int64, 0, len(h.Data))
		for _, msg := range h.Data {
			seqs = append(seqs, msg.Seq)
		}
		fmt.Printf("history page=%d before_seq=%d seq=%v has_more=%v next_before_seq=%d\n", page, before, seqs, h.HasMore, h.NextBeforeSeq)
		if page == 1 && (!h.HasMore || h.NextBeforeSeq <= 0) {
			return fmt.Errorf("history pagination not available")
		}
		if page == 2 && len(seqs) > 0 && seqs[len(seqs)-1] >= before {
			return fmt.Errorf("history page 2 did not move backward")
		}
		before = h.NextBeforeSeq
	}
	return nil
}

func waitACKs(ctx context.Context, rdb *redis.Client, uid string, ids []string) error {
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		allGone := true
		for _, id := range ids {
			_, err := rdb.ZScore(ctx, "pending_ack:"+uid, id).Result()
			if err == nil {
				allGone = false
				break
			}
			if err != redis.Nil {
				return err
			}
		}
		if allGone {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("pending ACK entries still exist for user %s: %v", uid, ids)
}
