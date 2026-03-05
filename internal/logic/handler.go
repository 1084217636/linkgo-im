package logic

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/1084217636/linkgo-im/api"
	"github.com/redis/go-redis/v9"
	_ "github.com/go-sql-driver/mysql"
)

// LogicHandler 结构体
type LogicHandler struct {
	api.UnimplementedLogicServer // ⚠️ 必须加这一行，否则 main.go 注册 gRPC 时必报错！
	
	Rdb *redis.Client
	DB  *sql.DB
}

// MsgPayload 定义消息传输载荷
type MsgPayload struct {
	From   string `json:"from"`
	To     string `json:"to"`
	ToType string `json:"to_type"` // "user" 或 "group"
	Msg    string `json:"msg"`
}

// PushMessage 消息推送逻辑
func (h *LogicHandler) PushMessage(ctx context.Context, req *api.PushMsgReq) (*api.PushMsgReply, error) {
	if string(req.Content) == "PING" {
		return &api.PushMsgReply{}, nil
	}

	var payload MsgPayload
	if err := json.Unmarshal(req.Content, &payload); err != nil {
		fmt.Printf("解析消息失败: %v\n", err)
		return &api.PushMsgReply{}, nil
	}

	if payload.From == "" {
		payload.From = req.UserId
	}

	// 序列化为 JSON 字符串存储
	historyData, _ := json.Marshal(payload)

	// Redis 快速缓存
	if payload.ToType == "group" {
		h.Rdb.LPush(ctx, "hist:"+payload.To, string(historyData))
		h.Rdb.LTrim(ctx, "hist:"+payload.To, 0, 99)
	} else {
		h.Rdb.LPush(ctx, "hist:"+payload.From+":"+payload.To, string(historyData))
		h.Rdb.LPush(ctx, "hist:"+payload.To+":"+payload.From, string(historyData))
	}

	// 消息分发
	h.dispatch(ctx, payload.To, string(historyData))

	// MySQL 持久化
	go h.saveMessage(payload.From, payload)

	return &api.PushMsgReply{}, nil
}

func (h *LogicHandler) dispatch(ctx context.Context, targetId string, content string) {
	isOnline, _ := h.Rdb.Exists(ctx, "route:"+targetId).Result()
	if isOnline > 0 {
		broadcastData := fmt.Sprintf("%s:%s", targetId, content)
		h.Rdb.Publish(ctx, "im_message_push", broadcastData)
	} else {
		h.Rdb.ZAdd(ctx, "offline_msg:"+targetId, redis.Z{
			Score:  float64(time.Now().UnixMilli()),
			Member: content,
		})
	}
}

func (h *LogicHandler) saveMessage(from string, p MsgPayload) {
	if h.DB == nil { return }
	sessionId := p.To
	if p.ToType == "user" {
		ids := []string{from, p.To}
		sort.Strings(ids)
		sessionId = strings.Join(ids, "_")
	}
	query := "INSERT INTO messages (session_id, from_uid, to_id, to_type, content, create_time) VALUES (?, ?, ?, ?, ?, ?)"
	h.DB.Exec(query, sessionId, from, p.To, p.ToType, p.Msg, time.Now().UnixMilli())
}

// GetHistory 拉取历史记录
func (h *LogicHandler) GetHistory(ctx context.Context, req *api.GetHistoryReq) (*api.GetHistoryReply, error) {
	var sessionId string
	if strings.HasPrefix(req.TargetId, "G") {
		sessionId = req.TargetId
	} else {
		uids := []string{req.UserId, req.TargetId}
		sort.Strings(uids)
		sessionId = strings.Join(uids, "_")
	}

	rows, err := h.DB.QueryContext(ctx, 
		"SELECT from_uid, to_id, to_type, content FROM messages WHERE session_id = ? ORDER BY id DESC LIMIT 20", 
		sessionId)
	if err != nil { return nil, err }
	defer rows.Close()

	var res []string
	for rows.Next() {
		var f_uid, t_id, t_type, content string
		if err := rows.Scan(&f_uid, &t_id, &t_type, &content); err == nil {
			m := MsgPayload{From: f_uid, To: t_id, ToType: t_type, Msg: content}
			data, _ := json.Marshal(m)
			res = append(res, string(data))
		}
	}
	for i, j := 0, len(res)-1; i < j; i, j = i+1, j-1 {
		res[i], res[j] = res[j], res[i]
	}
	return &api.GetHistoryReply{Messages: res}, nil
}