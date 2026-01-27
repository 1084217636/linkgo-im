package repo

import (
	"context"
	"github.com/IBM/sarama"
	"github.com/redis/go-redis/v9"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

// 定义全局的 DB 对象，供这一层使用
var (
	db            *gorm.DB
	rdb           *redis.Client
	kafkaProducer sarama.SyncProducer
)

// InitData 初始化所有连接 (被 main 调用)
func InitData() {
	// 初始化 MySQL
	var err error
	dsn := "root:123456@tcp(127.0.0.1:3306)/linkgo_im?charset=utf8mb4&parseTime=True&loc=Local"
	db, err = gorm.Open(mysql.Open(dsn), &gorm.Config{})
	if err != nil { panic(err) }

	// 初始化 Redis
	rdb = redis.NewClient(&redis.Options{Addr: "127.0.0.1:6379", Password: "123456"})

	// 初始化 Kafka
	kConfig := sarama.NewConfig()
	kConfig.Producer.RequiredAcks = sarama.WaitForAll
	kConfig.Producer.Partitioner = sarama.NewHashPartitioner
	kConfig.Producer.Return.Successes = true
	kafkaProducer, err = sarama.NewSyncProducer([]string{"127.0.0.1:9092"}, kConfig)
	if err != nil { panic(err) }
}

// --- 下面是封装好的原子操作 ---

func GetNextSeq(ctx context.Context, key string) (int64, error) {
	return rdb.Incr(ctx, key).Result()
}

func GetUserRoute(ctx context.Context, userId string) (string, error) {
	return rdb.Get(ctx, "route:"+userId).Result()
}

func SendToKafka(topic string, key string, value []byte) error {
	msg := &sarama.ProducerMessage{
		Topic: topic,
		Key:   sarama.StringEncoder(key),
		Value: sarama.ByteEncoder(value),
	}
	_, _, err := kafkaProducer.SendMessage(msg)
	return err
}

// 对应 chat_histories 表的查询
type ChatHistory struct {
	ID      uint   `gorm:"primaryKey"`
	UserId  string `gorm:"column:user_id"`
	Content []byte `gorm:"column:content"`
    Seq     int64  `gorm:"column:seq"`
}
func (ChatHistory) TableName() string { return "chat_histories" }

func GetHistoryMsgs(userId string, limit int) []ChatHistory {
	var msgs []ChatHistory
	db.Where("user_id = ?", userId).Order("seq desc").Limit(limit).Find(&msgs)
	return msgs
}