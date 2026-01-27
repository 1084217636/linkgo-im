// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2

package logic

import (
	"context"
	"fmt"

	"github.com/1084217636/linkgo-im/app/user/api/internal/svc"
	"github.com/1084217636/linkgo-im/app/user/api/internal/types"
	"github.com/1084217636/linkgo-im/app/user/model"
	"github.com/zeromicro/go-zero/core/logx"
)

type RegisterLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewRegisterLogic(ctx context.Context, svcCtx *svc.ServiceContext) *RegisterLogic {
	return &RegisterLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *RegisterLogic) Register(req *types.RegisterReq) (resp *types.RegisterResp, err error) {
    fmt.Printf(">>> 收到注册请求！用户名: %s\n", req.Username)

    // 1. 构建要插入的数据对象
    newUser := &model.User{
        Username: req.Username,
        Password: req.Password, // 实际项目中这里要加密！比如用 md5 或 bcrypt
        Mobile:   req.Mobile,
    }

    // 2. 调用 Model 层的 Insert 方法写入数据库
    // l.svcCtx.UserModel 就是我们在 svc 里初始化的那个
    res, err := l.svcCtx.UserModel.Insert(l.ctx, newUser)
    if err != nil {
        return nil, err // 如果插入失败（比如用户名重复），直接返回错误
    }

    // 3. 获取刚刚插入的 ID
    userId, _ := res.LastInsertId()

    // 4. 返回成功结果
    return &types.RegisterResp{
        UserId: fmt.Sprintf("%d", userId),
        Token:  "token-from-db-" + req.Username,
    }, nil
}