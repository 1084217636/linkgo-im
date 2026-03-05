package main

import (
	"database/sql"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"

	"github.com/1084217636/linkgo-im/api"
	"github.com/1084217636/linkgo-im/internal/logic"
	_ "github.com/go-sql-driver/mysql"
	"github.com/redis/go-redis/v9"
	"google.golang.org/grpc"
)

func getEnv(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok {
		return value
	}
	return fallback
}

func main() {
	redisAddr := getEnv("REDIS_ADDR", "127.0.0.1:6379")
	rpcPort := getEnv("RPC_PORT", "9001")
	dsn := getEnv("DB_DSN", "root:root@tcp(127.0.0.1:3306)/linkgo_im?charset=utf8mb4&parseTime=True")

	rdb := redis.NewClient(&redis.Options{
		Addr:     redisAddr,
		Password: "123456", 
		DB:       0,
	})

	db, err := sql.Open("mysql", dsn)
	if err != nil {
		log.Fatalf("❌ MySQL 连接驱动加载失败: %v", err)
	}
	db.SetMaxOpenConns(100)
	db.SetMaxIdleConns(10)
	
	if err := db.Ping(); err != nil {
		log.Fatalf("❌ 无法连接到 MySQL 数据库: %v", err)
	}

	// 初始化 Handler
	handler := &logic.LogicHandler{
		Rdb: rdb,
		DB:  db,
	}

	lis, err := net.Listen("tcp", ":"+rpcPort)
	if err != nil {
		log.Fatalf("❌ 监听端口失败: %v", err)
	}

	// 注册 gRPC 服务 (只要 handler 里嵌了 UnimplementedLogicServer 这里就不会报错)
	server := grpc.NewServer()
	api.RegisterLogicServer(server, handler)

	fmt.Printf("🧠 [Logic Service] 启动成功，监听端口 :%s\n", rpcPort)

	go func() {
		if err := server.Serve(lis); err != nil {
			log.Fatalf("❌ gRPC 服务异常停止: %v", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	server.GracefulStop()
	db.Close()
	rdb.Close()
	fmt.Println("服务已安全退出。")
}