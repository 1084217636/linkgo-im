package svc

import (
	"database/sql"

	"github.com/1084217636/linkgo-im/cmd/logic/internal/config"
	"github.com/1084217636/linkgo-im/internal/delivery"
	corelogic "github.com/1084217636/linkgo-im/internal/logic"
	_ "github.com/go-sql-driver/mysql"
	"github.com/redis/go-redis/v9"
	"github.com/segmentio/kafka-go"
	"github.com/zeromicro/go-zero/core/logx"
)

type ServiceContext struct {
	Config      config.Config
	Rdb         *redis.Client
	DB          *sql.DB
	KafkaWriter *kafka.Writer
	Core        *corelogic.LogicHandler
}

func NewServiceContext(c config.Config) *ServiceContext {
	rdb := redis.NewClient(&redis.Options{
		Addr:     c.Redis.Addr,
		Password: c.Redis.Password,
		DB:       0,
	})

	db, err := sql.Open("mysql", c.Database.Dsn)
	if err != nil {
		logx.Must(err)
	}
	db.SetMaxOpenConns(100)
	db.SetMaxIdleConns(10)
	logx.Must(db.Ping())

	kafkaWriter := &kafka.Writer{
		Addr:         kafka.TCP(c.Kafka.Brokers...),
		Topic:        c.Kafka.GroupTopic,
		RequiredAcks: kafka.RequireOne,
		Balancer:     &kafka.Hash{},
	}

	core := &corelogic.LogicHandler{
		Rdb:             rdb,
		DB:              db,
		Delivery:        &delivery.RedisDelivery{Rdb: rdb},
		GroupDispatcher: &kafkaDispatcher{writer: kafkaWriter},
	}

	return &ServiceContext{
		Config:      c,
		Rdb:         rdb,
		DB:          db,
		KafkaWriter: kafkaWriter,
		Core:        core,
	}
}

func (s *ServiceContext) Close() {
	if s == nil {
		return
	}
	if s.KafkaWriter != nil {
		_ = s.KafkaWriter.Close()
	}
	if s.DB != nil {
		_ = s.DB.Close()
	}
	if s.Rdb != nil {
		_ = s.Rdb.Close()
	}
}
