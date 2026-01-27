package svc

import (
	"github.com/1084217636/linkgo-im/app/user/api/internal/config"
	"github.com/1084217636/linkgo-im/app/user/model" // 引入 model
	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

type ServiceContext struct {
	Config    config.Config
	UserModel model.UserModel // <--- 声明
}

func NewServiceContext(c config.Config) *ServiceContext {
	// 建立数据库连接
	conn := sqlx.NewMysql(c.DataSource)

	return &ServiceContext{
		Config:    c,
		UserModel: model.NewUserModel(conn), // <--- 只需要 conn 即可
	}
}