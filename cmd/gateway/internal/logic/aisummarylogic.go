package logic

import (
	"context"
	"errors"
	"time"

	gwmiddleware "github.com/1084217636/linkgo-im/cmd/gateway/internal/middleware"
	"github.com/1084217636/linkgo-im/cmd/gateway/internal/svc"
	"github.com/1084217636/linkgo-im/cmd/gateway/internal/types"
	"github.com/1084217636/linkgo-im/internal/ai"
	"github.com/1084217636/linkgo-im/internal/metrics"
	"github.com/zeromicro/go-zero/core/logx"
)

type AISummaryLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewAISummaryLogic(ctx context.Context, svcCtx *svc.ServiceContext) *AISummaryLogic {
	return &AISummaryLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *AISummaryLogic) Generate(req *types.AISummaryReq) (*types.AISummaryResp, error) {
	userID := gwmiddleware.UserIDFromContext(l.ctx)
	providerName := "unknown"
	if l.svcCtx.AIProvider != nil {
		providerName = l.svcCtx.AIProvider.Name()
	}

	timeoutSeconds := l.svcCtx.Config.AI.TimeoutSeconds
	if timeoutSeconds <= 0 {
		timeoutSeconds = 10
	}
	ctx, cancel := context.WithTimeout(l.ctx, time.Duration(timeoutSeconds)*time.Second)
	defer cancel()

	result, err := l.svcCtx.AISummary.Generate(ctx, ai.GenerateSummaryParams{
		GroupID:      req.GroupID,
		OperatorID:   userID,
		MessageLimit: req.MessageLimit,
		IncludeTodos: req.IncludeTodos,
		IncludeRisks: req.IncludeRisks,
	})
	if err != nil {
		metrics.AISummaryRequests.WithLabelValues(providerName, aiSummaryMetricResult(err)).Inc()
		return nil, err
	}
	metrics.AISummaryRequests.WithLabelValues(result.Provider, "success").Inc()
	return toAISummaryResp(result), nil
}

func toAISummaryResp(result *ai.SummaryResult) *types.AISummaryResp {
	resp := &types.AISummaryResp{
		SummaryID:       result.SummaryID,
		GroupID:         result.GroupID,
		ConversationID:  result.ConversationID,
		MessageStartSeq: result.MessageStartSeq,
		MessageEndSeq:   result.MessageEndSeq,
		Summary:         result.Summary,
		Todos:           make([]types.AITodoItem, 0, len(result.Todos)),
		Risks:           make([]types.AIRiskItem, 0, len(result.Risks)),
		Provider:        result.Provider,
		CreatedAt:       result.CreatedAt,
	}
	for _, item := range result.Todos {
		resp.Todos = append(resp.Todos, types.AITodoItem{
			Title:     item.Title,
			OwnerID:   item.OwnerID,
			SourceSeq: item.SourceSeq,
		})
	}
	for _, item := range result.Risks {
		resp.Risks = append(resp.Risks, types.AIRiskItem{
			Level:       item.Level,
			Description: item.Description,
			SourceSeq:   item.SourceSeq,
		})
	}
	return resp
}

func aiSummaryMetricResult(err error) string {
	switch {
	case err == nil:
		return "success"
	case errors.Is(err, ai.ErrDatabaseRequired):
		return "db_missing"
	case errors.Is(err, ai.ErrGroupIDRequired), errors.Is(err, ai.ErrOperatorRequired):
		return "invalid"
	case errors.Is(err, ai.ErrForbidden):
		return "forbidden"
	case errors.Is(err, ai.ErrNoMessages):
		return "no_messages"
	default:
		return "error"
	}
}
