package main

import (
	"context"
	"fmt"
	"log"
	"os" // [修改点] 引入 os 包读取环境变量
	"time"

	"github.com/1084217636/linkgo-im/api"
	"github.com/IBM/sarama"
	"github.com/redis/go-redis/v9"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/proto"
)

// 配置常量
// [修改点] 地址不再是 const，而是通过函数动态获取
const (
	Topic = "group_msg"
)

// [修改点] 新增一个辅助函数：读取环境变量，读不到就用默认值
func getEnv(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok {
		return value
	}
	return fallback
}

func main() {
	// [修改点] 获取配置（这就是云原生的第一步）
	// 如果在 Docker 里跑，我们会注入 KAFKA_ADDR=kafka:9092
	// 如果在本地跑，没注入变量，就默认用 127.0.0.1:9092
	kafkaAddr := getEnv("KAFKA_ADDR", "127.0.0.1:9092")
	redisAddr := getEnv("REDIS_ADDR", "127.0.0.1:6379")
	redisPwd := getEnv("REDIS_PWD", "123456") // 密码也建议做成配置

	fmt.Printf("⚙️ 配置加载完毕: Redis=%s, Kafka=%s\n", redisAddr, kafkaAddr)

	// 1. 初始化 Redis
	rdb := redis.NewClient(&redis.Options{
		Addr:     redisAddr, // [修改点] 使用变量
		Password: redisPwd,  // [修改点] 使用变量
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
	
	// 重试逻辑保持不变
	for {
		// [修改点] 使用变量 kafkaAddr
		consumer, err = sarama.NewConsumer([]string{kafkaAddr}, config)
		if err != nil {
			fmt.Printf("⚠️ Kafka 连接失败 (%v) - 地址: %s, 3秒后重试...\n", err, kafkaAddr)
			time.Sleep(3 * time.Second)
			continue
		}
		break
	}
	fmt.Println("📚 Kafka 连接成功")
	defer consumer.Close()

	// 3. 订阅分区 (逻辑不变)
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

	// 4. 消费循环 (逻辑不变)
	for msg := range partitionConsumer.Messages() {
		chatMsg := &api.ChatMessage{}
		err := proto.Unmarshal(msg.Value, chatMsg)
		if err != nil {
			fmt.Printf("❌ 消息解析失败: %v\n", err)
			continue
		}

		if chatMsg.ToUserId != "" {
			pushToUser(rdb, chatMsg.ToUserId, msg.Value)
		} else if chatMsg.GroupId != "" {
			fmt.Printf("   [群消息] %s 发到群 %s\n", chatMsg.FromUserId, chatMsg.GroupId)
		}
	}
}

// pushToUser 逻辑不变，直接粘贴即可
func pushToUser(rdb *redis.Client, targetUserId string, content []byte) {
	ctx := context.Background()

	gatewayAddr, err := rdb.Get(ctx, "route:"+targetUserId).Result()

	if err == redis.Nil {
		fmt.Printf("   👤 用户 %s 不在线，正在存入离线信箱...\n", targetUserId)
		offlineKey := "offline:" + targetUserId
		err := rdb.RPush(ctx, offlineKey, content).Err()
		if err != nil {
			fmt.Printf("   ❌ 离线存储失败: %v\n", err)
		} else {
			len, _ := rdb.LLen(ctx, offlineKey).Result()
			fmt.Printf("   📦 成功存入信箱! (Key: %s, 当前信件数: %d)\n", offlineKey, len)
		}
		return
	}

	if err != nil {
		fmt.Printf("   ❌ 查路由报错: %v\n", err)
		return
	}

	// 注意：这里的 gatewayAddr 其实将来也可能需要考虑内网通信问题
	// 但目前它是存在 Redis 里的，暂时不动它
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