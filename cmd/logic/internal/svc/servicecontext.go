package svc

import (
	"context"
	"database/sql"
	"sync"
	"time"

	"github.com/1084217636/linkgo-im/cmd/logic/internal/config"
	"github.com/1084217636/linkgo-im/internal/ai"
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
	cancel      context.CancelFunc
	wg          sync.WaitGroup
}

func NewServiceContext(c config.Config) *ServiceContext {
	rdb := redis.NewClient(&redis.Options{
		Addr:     c.Cache.Addr,
		Password: c.Cache.Password,
		DB:       0,
	})

	db, err := sql.Open("mysql", c.Database.Dsn)
	if err != nil {
		logx.Must(err)
	}
	maxOpen := c.Database.MaxOpenConns
	if maxOpen <= 0 {
		maxOpen = 100
	}
	maxIdle := c.Database.MaxIdleConns
	if maxIdle <= 0 {
		maxIdle = 20
	}
	if maxIdle > maxOpen {
		maxIdle = maxOpen
	}
	connMaxLifetime := c.Database.ConnMaxLifetimeSeconds
	if connMaxLifetime <= 0 {
		connMaxLifetime = 300
	}
	connMaxIdleTime := c.Database.ConnMaxIdleTimeSeconds
	if connMaxIdleTime <= 0 {
		connMaxIdleTime = 60
	}
	db.SetMaxOpenConns(maxOpen)
	db.SetMaxIdleConns(maxIdle)
	db.SetConnMaxLifetime(time.Duration(connMaxLifetime) * time.Second)
	db.SetConnMaxIdleTime(time.Duration(connMaxIdleTime) * time.Second)
	logx.Must(db.Ping())

	kafkaWriter := &kafka.Writer{
		Addr:         kafka.TCP(c.Kafka.Brokers...),
		Topic:        c.Kafka.GroupTopic,
		RequiredAcks: kafka.RequireOne,
		Balancer:     &kafka.Hash{},
	}
	aiProvider := ai.NewProviderWithOptions(ai.ProviderOptions{
		Name:           c.AI.Provider,
		Model:          c.AI.Model,
		BaseURL:        c.AI.BaseURL,
		APIKey:         c.AI.APIKey,
		Timeout:        time.Duration(c.AI.TimeoutSeconds) * time.Second,
		FallbackToMock: c.AI.FallbackToMock,
	})
	knowledgeBase, err := ai.NewKnowledgeBase(c.AI.KnowledgePaths)
	if err != nil {
		logx.Errorf("load logic ai knowledge base: %v", err)
	}
	botID := c.AI.BotUserID
	if botID == "" {
		botID = corelogic.DefaultAIBotUserID
	}

	core := &corelogic.LogicHandler{
		Rdb:                rdb,
		DB:                 db,
		Delivery:           &delivery.RedisDelivery{Rdb: rdb},
		GroupDispatcher:    &kafkaDispatcher{writer: kafkaWriter},
		ConversationOutbox: true,
		BotResponder: &aiBotResponder{
			botID: botID,
			ask:   ai.NewAskService(db, aiProvider, knowledgeBase, c.AI.KnowledgeTopK),
		},
	}

	workerCtx, cancel := context.WithCancel(context.Background())
	service := &ServiceContext{
		Config:      c,
		Rdb:         rdb,
		DB:          db,
		KafkaWriter: kafkaWriter,
		Core:        core,
		cancel:      cancel,
	}
	service.wg.Add(1)
	go service.runConversationOutbox(workerCtx)
	return service
}

func (s *ServiceContext) runConversationOutbox(ctx context.Context) {
	defer s.wg.Done()
	process := func() {
		workCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		if _, err := s.Core.ProcessConversationOutbox(workCtx, 100); err != nil {
			logx.Errorw("conversation outbox worker failed", logx.Field("error", err.Error()))
		}
	}
	process()
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			process()
		}
	}
}

func (s *ServiceContext) Close() {
	if s == nil {
		return
	}
	if s.cancel != nil {
		s.cancel()
		s.wg.Wait()
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
