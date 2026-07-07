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

type AIAskLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewAIAskLogic(ctx context.Context, svcCtx *svc.ServiceContext) *AIAskLogic {
	return &AIAskLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *AIAskLogic) Ask(req *types.AIAskReq) (*types.AIAskResp, error) {
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

	start := time.Now()
	result, err := l.svcCtx.AIAsk.Ask(ctx, ai.AskParams{
		OperatorID: userID,
		Question:   req.Question,
		TopK:       req.TopK,
	})
	if err != nil {
		metricResult := aiAskMetricResult(err)
		metrics.AIAskRequests.WithLabelValues(providerName, metricResult).Inc()
		metrics.AIProviderLatencySeconds.WithLabelValues(providerName, metricResult).Observe(time.Since(start).Seconds())
		return nil, err
	}
	metrics.AIAskRequests.WithLabelValues(result.Provider, "success").Inc()
	metrics.AIProviderLatencySeconds.WithLabelValues(result.Provider, "success").Observe(time.Since(start).Seconds())
	return toAIAskResp(result), nil
}

func toAIAskResp(result *ai.AskResult) *types.AIAskResp {
	resp := &types.AIAskResp{
		AnswerID:      result.AnswerID,
		Question:      result.Question,
		Answer:        result.Answer,
		Sources:       make([]types.AIKnowledgeSource, 0, len(result.Sources)),
		KnowledgeHits: result.KnowledgeHits,
		Provider:      result.Provider,
		CreatedAt:     result.CreatedAt,
	}
	for _, item := range result.Sources {
		resp.Sources = append(resp.Sources, types.AIKnowledgeSource{
			Path:    item.Path,
			Title:   item.Title,
			Snippet: item.Snippet,
			Score:   item.Score,
		})
	}
	return resp
}

func aiAskMetricResult(err error) string {
	switch {
	case err == nil:
		return "success"
	case errors.Is(err, ai.ErrDatabaseRequired):
		return "db_missing"
	case errors.Is(err, ai.ErrQuestionRequired), errors.Is(err, ai.ErrOperatorRequired):
		return "invalid"
	default:
		return "error"
	}
}
