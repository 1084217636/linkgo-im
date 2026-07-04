package logic

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	gwmiddleware "github.com/1084217636/linkgo-im/cmd/gateway/internal/middleware"
	"github.com/1084217636/linkgo-im/cmd/gateway/internal/svc"
	"github.com/1084217636/linkgo-im/cmd/gateway/internal/types"
	corelogic "github.com/1084217636/linkgo-im/internal/logic"
	"github.com/1084217636/linkgo-im/internal/metrics"
	"github.com/zeromicro/go-zero/core/logx"
)

type RedPacketLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewRedPacketLogic(ctx context.Context, svcCtx *svc.ServiceContext) *RedPacketLogic {
	return &RedPacketLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *RedPacketLogic) Create(req *types.RedPacketCreateReq) (*types.RedPacketCreateResp, error) {
	if l.svcCtx.DB == nil {
		metrics.RedPacketOperations.WithLabelValues("create", "db_missing").Inc()
		return nil, errors.New("database is required")
	}
	senderID := gwmiddleware.UserIDFromContext(l.ctx)
	targetID := strings.TrimSpace(req.TargetID)
	toType := normalizeToType(req.ToType, targetID)
	conversationID := strings.TrimSpace(req.ConversationID)
	if conversationID == "" {
		conversationID = buildGatewayConversationID(senderID, targetID, toType)
	}
	if senderID == "" || targetID == "" || conversationID == "" {
		metrics.RedPacketOperations.WithLabelValues("create", "invalid").Inc()
		return nil, errors.New("target_id is required")
	}
	if err := l.validateConversationAccess(senderID, targetID, toType, conversationID); err != nil {
		metrics.RedPacketOperations.WithLabelValues("create", "forbidden").Inc()
		return nil, err
	}

	service := corelogic.NewRedPacketService(l.svcCtx.DB)
	packet, err := service.Create(l.ctx, corelogic.RedPacketCreateParams{
		SenderID:       senderID,
		ConversationID: conversationID,
		ToType:         toType,
		TotalAmount:    req.TotalAmount,
		TotalCount:     req.TotalCount,
		Greeting:       req.Greeting,
		ExpiresAt:      req.ExpiresAt,
	})
	if err != nil {
		metrics.RedPacketOperations.WithLabelValues("create", "error").Inc()
		return nil, err
	}
	metrics.RedPacketOperations.WithLabelValues("create", "success").Inc()
	return &types.RedPacketCreateResp{Packet: toRedPacketInfo(*packet)}, nil
}

func (l *RedPacketLogic) Claim(req *types.RedPacketClaimReq) (*types.RedPacketClaimResp, error) {
	if l.svcCtx.DB == nil {
		metrics.RedPacketOperations.WithLabelValues("claim", "db_missing").Inc()
		return nil, errors.New("database is required")
	}
	userID := gwmiddleware.UserIDFromContext(l.ctx)
	packetID := strings.TrimSpace(req.RedPacketID)
	if userID == "" || packetID == "" {
		metrics.RedPacketOperations.WithLabelValues("claim", "invalid").Inc()
		return nil, errors.New("red_packet_id is required")
	}

	service := corelogic.NewRedPacketService(l.svcCtx.DB)
	detail, err := service.Detail(l.ctx, packetID, userID)
	if err != nil {
		metrics.RedPacketOperations.WithLabelValues("claim", redPacketMetricResult(err)).Inc()
		return nil, err
	}
	if err := l.validatePacketParticipant(userID, detail.Packet); err != nil {
		metrics.RedPacketOperations.WithLabelValues("claim", "forbidden").Inc()
		return nil, err
	}

	claim, err := service.Claim(l.ctx, packetID, userID)
	if errors.Is(err, corelogic.ErrRedPacketAlreadyClaimed) && claim != nil {
		metrics.RedPacketOperations.WithLabelValues("claim", "already_claimed").Inc()
		return &types.RedPacketClaimResp{
			RedPacketID:    claim.RedPacketID,
			UserID:         claim.UserID,
			Amount:         claim.Amount,
			CreatedAt:      claim.CreatedAt,
			AlreadyClaimed: true,
			Status:         "already_claimed",
		}, nil
	}
	if err != nil {
		metrics.RedPacketOperations.WithLabelValues("claim", redPacketMetricResult(err)).Inc()
		return nil, err
	}
	metrics.RedPacketOperations.WithLabelValues("claim", "success").Inc()
	return &types.RedPacketClaimResp{
		RedPacketID: claim.RedPacketID,
		UserID:      claim.UserID,
		Amount:      claim.Amount,
		CreatedAt:   claim.CreatedAt,
		Status:      "claimed",
	}, nil
}

func (l *RedPacketLogic) Detail(req *types.RedPacketDetailReq) (*types.RedPacketDetailResp, error) {
	if l.svcCtx.DB == nil {
		return nil, errors.New("database is required")
	}
	userID := gwmiddleware.UserIDFromContext(l.ctx)
	service := corelogic.NewRedPacketService(l.svcCtx.DB)
	detail, err := service.Detail(l.ctx, req.RedPacketID, userID)
	if err != nil {
		return nil, err
	}
	if err := l.validatePacketParticipant(userID, detail.Packet); err != nil {
		return nil, err
	}
	resp := &types.RedPacketDetailResp{
		Packet:  toRedPacketInfo(detail.Packet),
		Claims:  make([]types.RedPacketClaimInfo, 0, len(detail.Claims)),
		Claimed: detail.Claimed,
	}
	for _, claim := range detail.Claims {
		resp.Claims = append(resp.Claims, types.RedPacketClaimInfo{
			RedPacketID: claim.RedPacketID,
			UserID:      claim.UserID,
			Amount:      claim.Amount,
			CreatedAt:   claim.CreatedAt,
		})
	}
	return resp, nil
}

func (l *RedPacketLogic) validateConversationAccess(uid, targetID, toType, conversationID string) error {
	if toType == "group" {
		return l.validateActiveGroupMember(uid, targetID)
	}
	if uid == targetID {
		return errors.New("cannot send red packet to yourself")
	}
	expected := buildGatewayConversationID(uid, targetID, "user")
	if expected != conversationID {
		return errors.New("conversation_id does not match target_id")
	}
	return l.validateNormalFriend(uid, targetID)
}

func (l *RedPacketLogic) validatePacketParticipant(uid string, packet corelogic.RedPacketInfo) error {
	if packet.ToType == "group" {
		groupID := strings.TrimPrefix(packet.ConversationID, "group:")
		if groupID == "" {
			return errors.New("invalid group conversation")
		}
		return l.validateActiveGroupMember(uid, groupID)
	}
	parts := strings.Split(packet.ConversationID, ":")
	if len(parts) != 3 || parts[0] != "c2c" {
		return errors.New("invalid user conversation")
	}
	if parts[1] != uid && parts[2] != uid {
		return errors.New("user is not in red packet conversation")
	}
	return nil
}

func (l *RedPacketLogic) validateNormalFriend(uid, friendID string) error {
	var status string
	err := l.svcCtx.DB.QueryRowContext(l.ctx, `
SELECT status
FROM friend_relations
WHERE user_id = ? AND friend_id = ?
LIMIT 1
`, uid, friendID).Scan(&status)
	if err == nil && status == "normal" {
		return nil
	}
	if errors.Is(err, sql.ErrNoRows) || status != "normal" {
		return errors.New("target user is not a normal friend")
	}
	return err
}

func (l *RedPacketLogic) validateActiveGroupMember(uid, groupID string) error {
	var status string
	var muteUntil int64
	err := l.svcCtx.DB.QueryRowContext(l.ctx, `
SELECT status, mute_until
FROM group_members
WHERE group_id = ? AND user_id = ?
LIMIT 1
`, groupID, uid).Scan(&status, &muteUntil)
	if err == nil && status == "active" && (muteUntil <= 0 || muteUntil <= time.Now().UnixMilli()) {
		return nil
	}
	if errors.Is(err, sql.ErrNoRows) || status != "active" {
		return errors.New("user is not an active group member")
	}
	return err
}

func toRedPacketInfo(packet corelogic.RedPacketInfo) types.RedPacketInfo {
	return types.RedPacketInfo{
		RedPacketID:     packet.ID,
		SenderID:        packet.SenderID,
		ConversationID:  packet.ConversationID,
		ToType:          packet.ToType,
		TotalAmount:     packet.TotalAmount,
		TotalCount:      packet.TotalCount,
		RemainingAmount: packet.RemainingAmount,
		RemainingCount:  packet.RemainingCount,
		Greeting:        packet.Greeting,
		Status:          packet.Status,
		CreatedAt:       packet.CreatedAt,
		ExpiresAt:       packet.ExpiresAt,
		UpdatedAt:       packet.UpdatedAt,
	}
}

func buildGatewayConversationID(uid, targetID, toType string) string {
	if uid == "" || targetID == "" {
		return ""
	}
	if normalizeToType(toType, targetID) == "group" {
		return "group:" + targetID
	}
	users := []string{uid, targetID}
	if users[0] > users[1] {
		users[0], users[1] = users[1], users[0]
	}
	return fmt.Sprintf("c2c:%s:%s", users[0], users[1])
}

func normalizeToType(toType, targetID string) string {
	toType = strings.ToLower(strings.TrimSpace(toType))
	if toType != "" {
		return toType
	}
	if strings.HasPrefix(targetID, "G") {
		return "group"
	}
	return "user"
}

func redPacketMetricResult(err error) string {
	switch {
	case err == nil:
		return "success"
	case errors.Is(err, corelogic.ErrRedPacketAlreadyClaimed):
		return "already_claimed"
	case errors.Is(err, corelogic.ErrRedPacketFinished):
		return "finished"
	case errors.Is(err, corelogic.ErrRedPacketExpired):
		return "expired"
	case errors.Is(err, corelogic.ErrRedPacketNotFound):
		return "not_found"
	default:
		return "error"
	}
}
