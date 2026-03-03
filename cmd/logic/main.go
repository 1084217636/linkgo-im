package main

import (
	"fmt"
	"net"
	"google.golang.org/grpc"
	"github.com/redis/go-redis/v9"
	"github.com/1084217636/linkgo-im/api"      // 确保路径正确
	"github.com/1084217636/linkgo-im/internal/logic"
)

func main() {
	// 1. 初始化 Redis
	rdb := redis.NewClient(&redis.Options{Addr: "127.0.0.1:6379", Password: "123456"})
	
	// 2. 初始化逻辑处理器 (实现你简历里的 ZSet 和 Pub/Sub)
	handler := &logic.LogicHandler{Rdb: rdb}

	// 3. 启动 gRPC 服务，接收来自 Gateway 的请求
	lis, err := net.Listen("tcp", ":9001")
	if err != nil {
		panic(err)
	}
	
	s := grpc.NewServer()
	// 注意：这里需要根据你的 proto 生成文件注册
	api.RegisterLogicServer(s, handler) 

	fmt.Println("🧠 [Logic] 业务服务已启动 (Port: 9001)...")
	s.Serve(lis)
}