package logic

import (
	"context"
	"errors"
	"strings"

	gwmiddleware "github.com/1084217636/linkgo-im/cmd/gateway/internal/middleware"
	"github.com/1084217636/linkgo-im/cmd/gateway/internal/svc"
	"github.com/1084217636/linkgo-im/cmd/gateway/internal/types"
	corelogic "github.com/1084217636/linkgo-im/internal/logic"
	"github.com/zeromicro/go-zero/core/logx"
)

type WalletLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewWalletLogic(ctx context.Context, svcCtx *svc.ServiceContext) *WalletLogic {
	return &WalletLogic{Logger: logx.WithContext(ctx), ctx: ctx, svcCtx: svcCtx}
}

func (l *WalletLogic) Detail() (*types.WalletDetailResp, error) {
	if l.svcCtx.DB == nil {
		return nil, errors.New("database is required")
	}
	userID := gwmiddleware.UserIDFromContext(l.ctx)
	wallet, ledger, err := corelogic.NewWalletService(l.svcCtx.DB).Get(l.ctx, userID)
	if err != nil {
		return nil, err
	}
	resp := &types.WalletDetailResp{
		Wallet: toWalletInfo(*wallet),
		Ledger: make([]types.WalletLedgerEntry, 0, len(ledger)),
	}
	for _, entry := range ledger {
		resp.Ledger = append(resp.Ledger, types.WalletLedgerEntry{
			ID:           entry.ID,
			UserID:       entry.UserID,
			Direction:    entry.Direction,
			Amount:       entry.Amount,
			BusinessType: entry.BusinessType,
			BusinessID:   entry.BusinessID,
			BalanceAfter: entry.BalanceAfter,
			CreatedAt:    entry.CreatedAt,
		})
	}
	return resp, nil
}

func (l *WalletLogic) MockRecharge(req *types.WalletRechargeReq) (*types.WalletRechargeResp, error) {
	if l.svcCtx.DB == nil {
		return nil, errors.New("database is required")
	}
	userID := gwmiddleware.UserIDFromContext(l.ctx)
	result, err := corelogic.NewWalletService(l.svcCtx.DB).MockRecharge(l.ctx, corelogic.MockRechargeParams{
		UserID:    userID,
		RequestID: strings.TrimSpace(req.RequestID),
		Channel:   strings.TrimSpace(req.Channel),
		Amount:    req.Amount,
	})
	if err != nil {
		return nil, err
	}
	return &types.WalletRechargeResp{
		OrderID:         result.OrderID,
		RequestID:       result.RequestID,
		Channel:         result.Channel,
		Amount:          result.Amount,
		ProviderTradeNo: result.ProviderTradeNo,
		AlreadyPaid:     result.AlreadyPaid,
		Wallet:          toWalletInfo(result.Wallet),
	}, nil
}

func toWalletInfo(wallet corelogic.WalletInfo) types.WalletInfo {
	return types.WalletInfo{
		UserID:           wallet.UserID,
		AvailableBalance: wallet.AvailableBalance,
		UpdatedAt:        wallet.UpdatedAt,
	}
}
