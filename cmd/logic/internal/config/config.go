package config

import "github.com/zeromicro/go-zero/zrpc"

type Config struct {
	zrpc.RpcServerConf
	Redis    RedisConf
	Database DatabaseConf
	Kafka    KafkaConf
	Auth     AuthConf
}

type RedisConf struct {
	Addr     string
	Password string
}

type DatabaseConf struct {
	Dsn string
}

type KafkaConf struct {
	Brokers    []string
	GroupTopic string
}

type AuthConf struct {
	AccessSecret string
}
