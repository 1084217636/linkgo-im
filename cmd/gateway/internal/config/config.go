package config

import "github.com/zeromicro/go-zero/rest"

type Config struct {
	rest.RestConf
	Redis   RedisConf
	Etcd    EtcdConf
	Logic   LogicConf
	Gateway GatewayConf
	Auth    AuthConf
}

type RedisConf struct {
	Addr     string
	Password string
}

type EtcdConf struct {
	Endpoints []string
}

type LogicConf struct {
	Addr string `json:",optional"`
}

type GatewayConf struct {
	ID              string `json:",optional"`
	RouteTTLSeconds int64  `json:",default=75"`
}

type AuthConf struct {
	AccessSecret string
}
