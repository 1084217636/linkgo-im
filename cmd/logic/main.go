package main

import (
	"fmt"
	"net"

	"github.com/1084217636/linkgo-im/api"
	"github.com/1084217636/linkgo-im/cmd/logic/handler"
	"github.com/1084217636/linkgo-im/cmd/logic/repo"
	"github.com/1084217636/linkgo-im/cmd/logic/service"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func main() {
	// 1. 初始化最底层的 Repo (数据库等)
	repo.InitData()
	fmt.Println("📚 数据层初始化完毕 (DB/Redis/Kafka)")

	// 2. 初始化 Gateway 客户端
	conn, _ := grpc.NewClient("127.0.0.1:9002", grpc.WithTransportCredentials(insecure.NewCredentials()))
	gatewayCli := api.NewGatewayClient(conn)

	// 3. 组装 Service (注入依赖)
	chatService := &service.ChatService{
		GatewayCli: gatewayCli,
	}

	// 4. 启动 Server (注入 Service)
	s := grpc.NewServer()
	api.RegisterLogicServer(s, &handler.LogicServer{
		ChatSvc: chatService,
	})
	// 修改前
	// lis, _ := net.Listen("tcp", ":9001")
	// fmt.Printf("🧠 Logic 服务 (分层架构版) 已启动: 9001")
	// s.Serve(lis)

	// 修改后 (用这段替换)
	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", 9001)) // 注意这里要检查 err
	if err != nil {
		panic(fmt.Sprintf("❌ 端口启动失败: %v", err))
	}
	fmt.Printf("🧠 Logic 服务 (分层架构版) 已启动: 9001\n")

	if err := s.Serve(lis); err != nil {
		panic(fmt.Sprintf("❌ gRPC 启动失败: %v", err))
	}
}