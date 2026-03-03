package logic

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/1084217636/linkgo-im/api"
	"github.com/redis/go-redis/v9"
)

type LogicHandler struct {
	api.UnimplementedLogicServer
	Rdb *redis.Client
}

// 用于解析前端 JSON
type MsgPayload struct {
	To  string `json:"to"`
	Msg string `json:"msg"`
}

func (h *LogicHandler) PushMessage(ctx context.Context, req *api.PushMsgReq) (*api.PushMsgReply, error) {
	var payload MsgPayload
	if err := json.Unmarshal(req.Content, &payload); err != nil {
		return &api.PushMsgReply{}, nil
	}

	targetId := payload.To
	content := payload.Msg
	fmt.Printf("📩 路由: %s -> %s [%s]\n", req.UserId, targetId, content)

	// 1. 检查目标是否在线
	isOnline, _ := h.Rdb.Exists(ctx, "route:"+targetId).Result()

	if isOnline > 0 {
		// 【在线】通过广播推送 (目标ID:内容)
		data := fmt.Sprintf("%s:%s", targetId, content)
		h.Rdb.Publish(ctx, "im_message_push", data)
	} else {
		// 【离线】存入目标的 ZSet，绝不存发送者自己
		key := "offline_msg:" + targetId
		h.Rdb.ZAdd(ctx, key, redis.Z{
			Score:  float64(time.Now().UnixMilli()),
			Member: content,
		})
	}

	return &api.PushMsgReply{}, nil
}