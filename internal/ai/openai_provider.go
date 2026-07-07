package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

const defaultOpenAICompatibleBaseURL = "https://api.openai.com/v1"

type OpenAICompatibleConfig struct {
	ProviderName string
	Model        string
	BaseURL      string
	APIKey       string
	Timeout      time.Duration
}

type OpenAICompatibleProvider struct {
	name    string
	model   string
	baseURL string
	apiKey  string
	timeout time.Duration
	client  *http.Client
}

func NewOpenAICompatibleProvider(cfg OpenAICompatibleConfig) *OpenAICompatibleProvider {
	name := strings.TrimSpace(cfg.ProviderName)
	if name == "" {
		name = "openai-compatible"
	}
	model := strings.TrimSpace(cfg.Model)
	if model == "" {
		model = "gpt-4o-mini"
	}
	baseURL := strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	if baseURL == "" {
		baseURL = defaultOpenAICompatibleBaseURL
	}
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	return &OpenAICompatibleProvider{
		name:    name,
		model:   model,
		baseURL: baseURL,
		apiKey:  strings.TrimSpace(cfg.APIKey),
		timeout: timeout,
		client:  &http.Client{Timeout: timeout},
	}
}

func (p *OpenAICompatibleProvider) Name() string {
	return p.name
}

func (p *OpenAICompatibleProvider) Summarize(ctx context.Context, req SummaryRequest) (*SummaryResult, error) {
	start := time.Now()
	result, err := p.summarize(ctx, req)
	status := "success"
	errMessage := ""
	if err != nil {
		status = "error"
		errMessage = err.Error()
	}
	RecordProviderAttempt(ctx, ProviderAttempt{
		Provider:     p.Name(),
		Status:       status,
		DurationMs:   time.Since(start).Milliseconds(),
		ErrorMessage: errMessage,
	})
	return result, err
}

func (p *OpenAICompatibleProvider) summarize(ctx context.Context, req SummaryRequest) (*SummaryResult, error) {
	if p.apiKey == "" {
		return nil, errors.New("ai api key is required for openai-compatible provider")
	}
	body, err := json.Marshal(openAIChatRequest{
		Model: p.model,
		Messages: []openAIChatMessage{
			{Role: "system", Content: "你是企业研发协同 IM 的群聊助手。只输出 JSON，不要输出 Markdown。"},
			{Role: "user", Content: buildSummaryPrompt(req)},
		},
		Temperature: 0.2,
		ResponseFormat: map[string]string{
			"type": "json_object",
		},
	})
	if err != nil {
		return nil, err
	}

	callCtx, cancel := context.WithTimeout(ctx, p.timeout)
	defer cancel()
	httpReq, err := http.NewRequestWithContext(callCtx, http.MethodPost, p.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("ai provider returned %s", resp.Status)
	}

	var chatResp openAIChatResponse
	if err := json.NewDecoder(resp.Body).Decode(&chatResp); err != nil {
		return nil, err
	}
	if len(chatResp.Choices) == 0 {
		return nil, errors.New("ai provider returned no choices")
	}
	return parseOpenAISummary(req, chatResp.Choices[0].Message.Content, p.name), nil
}

type openAIChatRequest struct {
	Model          string              `json:"model"`
	Messages       []openAIChatMessage `json:"messages"`
	Temperature    float64             `json:"temperature,omitempty"`
	ResponseFormat map[string]string   `json:"response_format,omitempty"`
}

type openAIChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type openAIChatResponse struct {
	Choices []struct {
		Message openAIChatMessage `json:"message"`
	} `json:"choices"`
}

type providerSummaryPayload struct {
	Summary string     `json:"summary"`
	Todos   []TodoItem `json:"todos"`
	Risks   []RiskItem `json:"risks"`
}

func buildSummaryPrompt(req SummaryRequest) string {
	var b strings.Builder
	b.WriteString("请总结下面的群聊消息，输出 JSON：{\"summary\":\"...\",\"todos\":[{\"title\":\"...\",\"owner_id\":\"...\",\"source_seq\":1}],\"risks\":[{\"level\":\"low|medium|high\",\"description\":\"...\",\"source_seq\":1}]}。\n")
	if !req.IncludeTodos {
		b.WriteString("todos 必须为空数组。\n")
	}
	if !req.IncludeRisks {
		b.WriteString("risks 必须为空数组。\n")
	}
	for _, msg := range req.Messages {
		b.WriteString(fmt.Sprintf("seq=%d from=%s content=%q\n", msg.Seq, msg.FromUID, msg.Content))
	}
	return b.String()
}

func parseOpenAISummary(req SummaryRequest, raw, provider string) *SummaryResult {
	payload := providerSummaryPayload{}
	if err := json.Unmarshal([]byte(raw), &payload); err != nil || strings.TrimSpace(payload.Summary) == "" {
		payload.Summary = strings.TrimSpace(raw)
	}
	result := &SummaryResult{
		GroupID:        req.GroupID,
		ConversationID: req.ConversationID,
		Summary:        payload.Summary,
		Todos:          payload.Todos,
		Risks:          payload.Risks,
		Provider:       provider,
	}
	if len(req.Messages) > 0 {
		result.MessageStartSeq = req.Messages[0].Seq
		result.MessageEndSeq = req.Messages[len(req.Messages)-1].Seq
	}
	if !req.IncludeTodos {
		result.Todos = nil
	}
	if !req.IncludeRisks {
		result.Risks = nil
	}
	return result
}
