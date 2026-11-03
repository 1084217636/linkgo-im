package logic

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"errors"
	"fmt"
	"github.com/1084217636/linkgo-im/api"
	"github.com/1084217636/linkgo-im/internal/middleware"
	"github.com/zeromicro/go-zero/core/logx"
	"golang.org/x/crypto/bcrypt"
	"strings"
)

func (h *LogicHandler) Login(ctx context.Context, req *api.LoginReq) (*api.LoginReply, error) {
	var uid string
	var pwdInDB string
	var status int

	query := "SELECT user_id, password, status FROM users WHERE username = ? LIMIT 1"
	if err := h.DB.QueryRowContext(ctx, query, req.Username).Scan(&uid, &pwdInDB, &status); err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			logx.Errorw("login query failed", logx.Field("error", err.Error()))
		}
		return nil, fmt.Errorf("invalid credentials")
	}

	valid, legacy := verifyPassword(pwdInDB, req.Password)
	if status != 1 || !valid {
		return nil, fmt.Errorf("invalid credentials")
	}
	if legacy {
		h.upgradeLegacyPassword(ctx, uid, pwdInDB, req.Password)
	}

	token, err := middleware.GenerateToken(uid) //生成JWT
	if err != nil {
		return nil, err
	}

	conversations, err := h.listConversations(ctx, uid, defaultConversationLimit)
	if err != nil {
		logx.Errorw("list conversations failed",
			logx.Field("target_id", uid),
			logx.Field("error", err.Error()),
		)
	}

	return &api.LoginReply{
		Token:         token,
		UserId:        uid,
		Conversations: conversations,
	}, nil
}

func verifyPassword(storedPassword, suppliedPassword string) (valid bool, legacy bool) {
	if isBcryptHash(storedPassword) {
		return bcrypt.CompareHashAndPassword([]byte(storedPassword), []byte(suppliedPassword)) == nil, false
	}
	return subtle.ConstantTimeCompare([]byte(storedPassword), []byte(suppliedPassword)) == 1, true
}

func isBcryptHash(password string) bool {
	return strings.HasPrefix(password, "$2a$") ||
		strings.HasPrefix(password, "$2b$") ||
		strings.HasPrefix(password, "$2y$")
}

func (h *LogicHandler) upgradeLegacyPassword(ctx context.Context, uid, storedPassword, suppliedPassword string) {
	hash, err := bcrypt.GenerateFromPassword([]byte(suppliedPassword), bcrypt.DefaultCost)
	if err != nil {
		logx.Errorw("hash legacy password failed", logx.Field("target_id", uid), logx.Field("error", err.Error()))
		return
	}
	if _, err := h.DB.ExecContext(ctx, `
UPDATE users SET password = ?
WHERE user_id = ? AND password = ?
`, string(hash), uid, storedPassword); err != nil {
		logx.Errorw("upgrade legacy password failed", logx.Field("target_id", uid), logx.Field("error", err.Error()))
	}
}
