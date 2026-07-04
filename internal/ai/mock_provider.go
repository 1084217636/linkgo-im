package ai

import (
	"context"
	"fmt"
	"strings"
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
	select {
	case <-ctx.Done():
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
