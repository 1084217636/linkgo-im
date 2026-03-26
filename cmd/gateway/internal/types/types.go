package types

import "github.com/1084217636/linkgo-im/api"

type LoginReq struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type LoginResp struct {
	Token  string `json:"token"`
	UserID string `json:"user_id"`
}

type HistoryReq struct {
	TargetID string `form:"target_id"`
}

type HistoryResp struct {
	Data []*api.WireMessage `json:"data"`
}

type GroupCreateReq struct {
	GroupID string   `json:"group_id"`
	Members []string `json:"members"`
}

type GroupCreateResp struct {
	GroupID string `json:"group_id"`
	Members int    `json:"members"`
	Msg     string `json:"msg"`
}

type UserGroupsResp struct {
	Groups []string `json:"groups"`
}
