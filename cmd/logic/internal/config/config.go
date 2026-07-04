package config

import "github.com/zeromicro/go-zero/zrpc"

type Config struct {
	zrpc.RpcServerConf
	Cache    RedisConf
	Database DatabaseConf
	Kafka    KafkaConf
	JWT      JWTConf
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
