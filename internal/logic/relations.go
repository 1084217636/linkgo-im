package logic

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/1084217636/linkgo-im/api"
	"github.com/go-sql-driver/mysql"
	"github.com/redis/go-redis/v9"
)

func (h *LogicHandler) validateSendPermission(ctx context.Context, frame *api.WireMessage) error {
	if frame == nil || h.DB == nil {
		return nil
	}
	if frame.ToType == "group" {
		ok, err := h.isActiveGroupMember(ctx, frame.To, frame.From)
		if err != nil {
			if isMissingRelationTable(err) {
				return h.validateGroupPermissionFromRedis(ctx, frame)
			}
			return err
		}
		if !ok {
			return fmt.Errorf("sender is not an active group member")
		}
		return nil
	}

	ok, err := h.isNormalFriend(ctx, frame.From, frame.To)
	if err != nil {
		if isMissingRelationTable(err) {
			return nil
		}
		return err
	}
	if !ok {
		return fmt.Errorf("target user is not a normal friend")
	}
	return nil
}

func (h *LogicHandler) isNormalFriend(ctx context.Context, uid, friendID string) (bool, error) {
	var status string
	err := h.DB.QueryRowContext(ctx, `
SELECT status
FROM friend_relations
WHERE user_id = ? AND friend_id = ?
LIMIT 1
`, uid, friendID).Scan(&status)
	if err == nil {
		return status == "normal", nil
	}
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return false, err
}

func (h *LogicHandler) isActiveGroupMember(ctx context.Context, groupID, uid string) (bool, error) {
	var status string
	var muteUntil int64
	err := h.DB.QueryRowContext(ctx, `
SELECT status, mute_until
FROM group_members
WHERE group_id = ? AND user_id = ?
LIMIT 1
`, groupID, uid).Scan(&status, &muteUntil)
	if err == nil {
		return status == "active" && (muteUntil <= 0 || muteUntil <= time.Now().UnixMilli()), nil
	}
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return false, err
}

func (h *LogicHandler) isCurrentGroupMember(ctx context.Context, groupID, uid string) (bool, error) {
	if h.DB == nil {
		return false, fmt.Errorf("group membership store is unavailable")
	}
	var status string
	err := h.DB.QueryRowContext(ctx, `
SELECT status
FROM group_members
WHERE group_id = ? AND user_id = ?
LIMIT 1
`, groupID, uid).Scan(&status)
	if err == nil {
		return status == "active", nil
	}
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return false, err
}

func (h *LogicHandler) validateGroupPermissionFromRedis(ctx context.Context, frame *api.WireMessage) error {
	if h.Rdb == nil || frame == nil {
		return nil
	}
	ok, err := h.Rdb.SIsMember(ctx, "group_members:"+frame.To, frame.From).Result()
	if err != nil && err != redis.Nil {
		return err
	}
	if !ok {
		return fmt.Errorf("sender is not a group member")
	}
	return nil
}

func (h *LogicHandler) loadGroupRecipientsFromDB(ctx context.Context, groupID, senderID string) ([]string, error) {
	rows, err := h.DB.QueryContext(ctx, `
SELECT user_id
FROM group_members
WHERE group_id = ? AND status = 'active' AND user_id <> ?
ORDER BY joined_at ASC
`, groupID, senderID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	recipients := make([]string, 0)
	for rows.Next() {
		var uid string
		if err := rows.Scan(&uid); err != nil {
			return nil, err
		}
		if uid != "" {
			recipients = append(recipients, uid)
		}
	}
	return recipients, rows.Err()
}

func isMissingRelationTable(err error) bool {
	if err == nil {
		return false
	}
	var mysqlErr *mysql.MySQLError
	if errors.As(err, &mysqlErr) {
		return mysqlErr.Number == 1146
	}
	return false
}
