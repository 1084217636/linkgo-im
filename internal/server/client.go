package server

import (
	"context"
	"time"
	"github.com/gorilla/websocket"
	"github.com/1084217636/linkgo-im/api" // 你的 protobuf
)

// StartClientLoop 处理长连接的读写逻辑
func StartClientLoop(uid string, conn *websocket.Conn, logic api.LogicClient) {
	// 对应简历：心跳检测机制
	readTimeout := 60 * time.Second
	conn.SetReadDeadline(time.Now().Add(readTimeout))

	for {
		// 1. 读取客户端消息
		_, msg, err := conn.ReadMessage()
		if err != nil {
			break // 异常或断开则退出循环，触发外部 defer 清理
		}

		// 2. 心跳续命
		conn.SetReadDeadline(time.Now().Add(readTimeout))
		if string(msg) == "PING" {
			conn.WriteMessage(websocket.TextMessage, []byte("PONG"))
			continue
		}

		// 3. 对应简历：解耦业务逻辑与长连接管理
		// 将消息异步/同步发送至 Logic 层处理
		go func(data []byte) {
			logic.PushMessage(context.Background(), &api.PushMsgReq{
				UserId:  uid,
				Content: data,
			})
		}(msg)
	}
}