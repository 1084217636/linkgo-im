package config

import (
	"github.com/zeromicro/go-zero/rest"
)

type Config struct {
	rest.RestConf
	Redis    RedisConf    `json:",optional"`
	Database DatabaseConf `json:",optional"`
	Etcd     EtcdConf     `json:",optional"`
	Logic    LogicConf    `json:",optional"`
	Gateway  GatewayConf  `json:",optional"`
	Auth     AuthConf     `json:",optional"`
	AI       AIConf       `json:",optional"`
}

type RedisConf struct {
	Addr     string `json:",optional"`
	Password string `json:",optional"`
}

type DatabaseConf struct {
	Dsn                    string `json:",optional"`
	MaxOpenConns           int    `json:",default=80"`
	MaxIdleConns           int    `json:",default=20"`
	ConnMaxLifetimeSeconds int64  `json:",default=300"`
	ConnMaxIdleTimeSeconds int64  `json:",default=60"`
}

type EtcdConf struct {
	Endpoints []string `json:",optional"`
}

type LogicConf struct {
	Addr string `json:",optional"`
}

type GatewayConf struct {
	ID                   string   `json:",optional"`
	AllowedOrigins       []string `json:",optional"`
	AllowMissingOrigin   bool     `json:",default=false"`
	RouteTTLSeconds      int64    `json:",default=75"`
	AckTimeoutSeconds    int64    `json:",default=5"`
	AckMaxRetries        int      `json:",default=3"`
	RetryIntervalSeconds int64    `json:",default=1"`
}

type AuthConf struct {
	AccessSecret string `json:",optional"`
}

type AIConf struct {
	Provider       string   `json:",optional"`
	Model          string   `json:",optional"`
	BaseURL        string   `json:",optional"`
	APIKey         string   `json:",optional"`
	TimeoutSeconds int64    `json:",default=10"`
	MaxMessages    int      `json:",default=100"`
	FallbackToMock bool     `json:",default=true"`
	KnowledgePaths []string `json:",optional"`
	KnowledgeTopK  int      `json:",default=3"`
}
