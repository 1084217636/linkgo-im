package config

import "github.com/zeromicro/go-zero/zrpc"

type Config struct {
	zrpc.RpcServerConf
	Cache    RedisConf
	Database DatabaseConf
	Kafka    KafkaConf
	JWT      JWTConf
	AI       AIConf `json:",optional"`
}

type RedisConf struct {
	Addr     string
	Password string
}

type DatabaseConf struct {
	Dsn                    string
	MaxOpenConns           int
	MaxIdleConns           int
	ConnMaxLifetimeSeconds int64
	ConnMaxIdleTimeSeconds int64
}

type KafkaConf struct {
	Brokers    []string
	GroupTopic string
}

type JWTConf struct {
	AccessSecret string
}

type AIConf struct {
	Provider       string   `json:",optional"`
	Model          string   `json:",optional"`
	BaseURL        string   `json:",optional"`
	APIKey         string   `json:",optional"`
	TimeoutSeconds int64    `json:",default=10"`
	FallbackToMock bool     `json:",default=true"`
	KnowledgePaths []string `json:",optional"`
	KnowledgeTopK  int      `json:",default=3"`
	BotUserID      string   `json:",optional"`
}
