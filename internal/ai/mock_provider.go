package ai

import (
	"context"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

const mockProviderName = "mock"

type MockProvider struct{}

func NewMockProvider() *MockProvider {
	return &MockProvider{}
}

func (p *MockProvider) Name() string {
	return mockProviderName
}

func (p *MockProvider) Summarize(ctx context.Context, req SummaryRequest) (*SummaryResult, error) {
	start := time.Now()
	status := "success"
	errMessage := ""
	defer func() {
		RecordProviderAttempt(ctx, ProviderAttempt{
			Provider:     p.Name(),
			Status:       status,
			DurationMs:   time.Since(start).Milliseconds(),
			ErrorMessage: errMessage,
		})
	}()
	select {
	case <-ctx.Done():
		status = "error"
		errMessage = ctx.Err().Error()
		return nil, ctx.Err()
	default:
	}

	result := &SummaryResult{
		GroupID:        req.GroupID,
		ConversationID: req.ConversationID,
		Provider:       p.Name(),
	}
	if len(req.Messages) == 0 {
		result.Summary = "本次群聊暂无可总结消息。"
		return result, nil
	}

	result.MessageStartSeq = req.Messages[0].Seq
	result.MessageEndSeq = req.Messages[len(req.Messages)-1].Seq
	result.Summary = buildMockSummary(req.Messages)
	if req.IncludeTodos {
		result.Todos = extractMockTodos(req.Messages)
	}
	if req.IncludeRisks {
		result.Risks = extractMockRisks(req.Messages)
	}
	return result, nil
}

func (p *MockProvider) Answer(ctx context.Context, req AskRequest) (*AskResult, error) {
	start := time.Now()
	status := "success"
	errMessage := ""
	defer func() {
		RecordProviderAttempt(ctx, ProviderAttempt{
			Provider:     p.Name(),
			Status:       status,
			DurationMs:   time.Since(start).Milliseconds(),
			ErrorMessage: errMessage,
		})
	}()
	select {
	case <-ctx.Done():
		status = "error"
		errMessage = ctx.Err().Error()
		return nil, ctx.Err()
	default:
	}

	result := &AskResult{
		Question:      req.Question,
		Sources:       req.Sources,
		KnowledgeHits: len(req.Sources),
		Provider:      p.Name(),
	}
	if len(req.Sources) == 0 {
		result.Answer = "知识库中暂未找到相关资料，请尝试提供更具体的模块名、链路或关键词。"
		return result, nil
	}

	result.Answer = buildMockAnswer(req.Question, req.Sources)
	return result, nil
}

func buildMockSummary(messages []Message) string {
	parts := make([]string, 0, 3)
	for _, msg := range messages {
		content := strings.TrimSpace(msg.Content)
		if content == "" {
			continue
		}
		parts = append(parts, truncateRunes(content, 32))
		if len(parts) == 3 {
			break
		}
	}
	if len(parts) == 0 {
		return fmt.Sprintf("本次群聊共 %d 条消息，主要为非文本或空内容。", len(messages))
	}
	return fmt.Sprintf("本次群聊共 %d 条消息，核心内容包括：%s。", len(messages), strings.Join(parts, "；"))
}

func extractMockTodos(messages []Message) []TodoItem {
	keywords := []string{"todo", "TODO", "待办", "需要", "请", "负责", "明天", "今天"}
	todos := make([]TodoItem, 0, 5)
	for _, msg := range messages {
		content := strings.TrimSpace(msg.Content)
		if content == "" || !containsAny(content, keywords) {
			continue
		}
		todos = append(todos, TodoItem{
			Title:     truncateRunes(content, 80),
			OwnerID:   msg.FromUID,
			SourceSeq: msg.Seq,
		})
		if len(todos) == 5 {
			break
		}
	}
	return todos
}

func extractMockRisks(messages []Message) []RiskItem {
	keywords := []string{"风险", "失败", "报错", "延迟", "超时", "blocked", "error", "fail"}
	risks := make([]RiskItem, 0, 5)
	for _, msg := range messages {
		content := strings.TrimSpace(msg.Content)
		if content == "" || !containsAny(content, keywords) {
			continue
		}
		risks = append(risks, RiskItem{
			Level:       "medium",
			Description: truncateRunes(content, 80),
			SourceSeq:   msg.Seq,
		})
		if len(risks) == 5 {
			break
		}
	}
	return risks
}

func buildMockAnswer(question string, sources []KnowledgeSource) string {
	parts := make([]string, 0, 2)
	references := make([]string, 0, 2)
	for _, source := range sources {
		snippet := strings.TrimSpace(source.Snippet)
		if snippet == "" {
			continue
		}
		parts = append(parts, truncateRunes(snippet, 80))
		references = append(references, source.Title)
		if len(parts) == 2 {
			break
		}
	}
	if len(parts) == 0 {
		return "知识库中找到了相关文档，但没有提取到有效摘要，请直接查看文档原文。"
	}
	return fmt.Sprintf("针对问题“%s”，知识库里的相关结论是：%s。建议优先查看：%s。", truncateRunes(strings.TrimSpace(question), 64), strings.Join(parts, "；"), strings.Join(references, "、"))
}

func containsAny(value string, keywords []string) bool {
	lowerValue := strings.ToLower(value)
	for _, keyword := range keywords {
		if strings.Contains(lowerValue, strings.ToLower(keyword)) {
			return true
		}
	}
	return false
}

func truncateRunes(value string, limit int) string {
	if limit <= 0 || utf8.RuneCountInString(value) <= limit {
		return value
	}
	runes := []rune(value)
	return string(runes[:limit]) + "..."
}
