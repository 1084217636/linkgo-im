package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/1084217636/linkgo-im/api"
	"github.com/IBM/sarama"
	"github.com/redis/go-redis/v9"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/proto"
)

// 配置常量
const (
	KafkaAddr = "127.0.0.1:9092"
	RedisAddr = "127.0.0.1:6379"
	Topic     = "group_msg"
)

func main() {
	// 1. 初始化 Redis
	rdb := redis.NewClient(&redis.Options{
		Addr:     RedisAddr,
		Password: "123456",
	})
	if _, err := rdb.Ping(context.Background()).Result(); err != nil {
		log.Fatalf("❌ Redis 连接失败: %v", err)
	}
	fmt.Println("📚 Redis 连接成功")

	// 2. 初始化 Kafka 消费者
	config := sarama.NewConfig()
	config.Consumer.Return.Errors = true

	var consumer sarama.Consumer
	var err error
	for {
		consumer, err = sarama.NewConsumer([]string{KafkaAddr}, config)
		if err != nil {
			fmt.Printf("⚠️ Kafka 连接失败 (%v), 3秒后重试...\n", err)
			time.Sleep(3 * time.Second)
			continue
		}
		break
	}
	fmt.Println("📚 Kafka 连接成功")
	defer consumer.Close()

	// 3. 订阅分区
	var partitionConsumer sarama.PartitionConsumer
	for {
		partitionConsumer, err = consumer.ConsumePartition(Topic, 0, sarama.OffsetNewest)
		if err != nil {
			fmt.Printf("⚠️ 获取分区失败: %v, 3秒后重试...\n", err)
			time.Sleep(3 * time.Second)
			continue
		}
		break
	}
	defer partitionConsumer.Close()

	fmt.Println("🚀 Transfer 服务已启动 (支持离线存储)...")

	// 4. 消费循环
	for msg := range partitionConsumer.Messages() {
		// 反序列化
		chatMsg := &api.ChatMessage{}
		err := proto.Unmarshal(msg.Value, chatMsg)
		if err != nil {
			fmt.Printf("❌ 消息解析失败: %v\n", err)
			continue
		}

		// 推送逻辑
		if chatMsg.ToUserId != "" {
			pushToUser(rdb, chatMsg.ToUserId, msg.Value)
		} else if chatMsg.GroupId != "" {
			// 群聊逻辑暂略，UserA -> UserB 测试属于单聊
			fmt.Printf("   [群消息] %s 发到群 %s\n", chatMsg.FromUserId, chatMsg.GroupId)
		}
	}
}

// pushToUser 核心修改：支持离线存储
func pushToUser(rdb *redis.Client, targetUserId string, content []byte) {
	ctx := context.Background()

	// 1. 尝试查找用户在线路由
	gatewayAddr, err := rdb.Get(ctx, "route:"+targetUserId).Result()

	// ---------------------------------------------------------
	// 【Day 14 核心代码】用户不在线 -> 存离线消息
	// ---------------------------------------------------------
	if err == redis.Nil {
		fmt.Printf("   👤 用户 %s 不在线，正在存入离线信箱...\n", targetUserId)

		// 使用 Redis List (RPUSH) 存储消息
		// Key 格式: "offline:UserB"
		offlineKey := "offline:" + targetUserId
		
		err := rdb.RPush(ctx, offlineKey, content).Err()
		if err != nil {
			fmt.Printf("   ❌ 离线存储失败: %v\n", err)
		} else {
			// 打印一下当前信箱里有多少封信
			len, _ := rdb.LLen(ctx, offlineKey).Result()
			fmt.Printf("   📦 成功存入信箱! (Key: %s, 当前信件数: %d)\n", offlineKey, len)
		}
		return
	}
	// ---------------------------------------------------------

	if err != nil {
		fmt.Printf("   ❌ 查路由报错: %v\n", err)
		return
	}

	// 2. 用户在线 -> 直接通过 gRPC 推送
	conn, err := grpc.NewClient(gatewayAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return
	}
	defer conn.Close()

	gatewayCli := api.NewGatewayClient(conn)
	_, err = gatewayCli.PushToUser(ctx, &api.SendToUserReq{
		TargetUserId: targetUserId,
		Content:      content,
	})

	if err != nil {
		fmt.Printf("   ❌ 推送异常: %v\n", err)
	} else {
		fmt.Printf("   ✅ 实时推送成功 (Gateway: %s)\n", gatewayAddr)
	}
}