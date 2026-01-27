// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2

package config

import "github.com/zeromicro/go-zero/rest"

type Config struct {
    rest.RestConf
    DataSource string // <--- 新增这一行，名字要和 yaml 里的一样
}