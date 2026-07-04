package logic

import (
	"context"
	"errors"
	"strings"

	"github.com/1084217636/linkgo-im/cmd/gateway/internal/svc"
	"github.com/1084217636/linkgo-im/cmd/gateway/internal/types"
	"github.com/zeromicro/go-zero/core/logx"
)

type GroupMembersLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewGroupMembersLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GroupMembersLogic {
	return &GroupMembersLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *GroupMembersLogic) List(req *types.GroupMembersReq) (*types.GroupMembersResp, error) {
	groupID := strings.TrimSpace(req.GroupID)
	if groupID == "" {
		return nil, errors.New("group_id is required")
	}
	if l.svcCtx.DB == nil {
		members, err := l.svcCtx.Rdb.SMembers(l.ctx, "group_members:"+groupID).Result()
		if err != nil {
			return nil, err
		}
		resp := &types.GroupMembersResp{GroupID: groupID, Members: make([]types.GroupMemberInfo, 0, len(members))}
		for _, member := range members {
			resp.Members = append(resp.Members, types.GroupMemberInfo{UserID: member, Role: "member", Status: "active"})
		}
		return resp, nil
	}

	rows, err := l.svcCtx.DB.QueryContext(l.ctx, `
SELECT user_id, role, status, mute_until, joined_at
FROM group_members
WHERE group_id = ? AND status = 'active'
ORDER BY role, joined_at
`, groupID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	resp := &types.GroupMembersResp{GroupID: groupID, Members: make([]types.GroupMemberInfo, 0)}
	for rows.Next() {
		var item types.GroupMemberInfo
		if err := rows.Scan(&item.UserID, &item.Role, &item.Status, &item.MuteUntil, &item.JoinedAt); err != nil {
			return nil, err
		}
		resp.Members = append(resp.Members, item)
	}
	return resp, rows.Err()
}
