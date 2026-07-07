package ai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestOpenAICompatibleProviderSummarize(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Fatalf("unexpected authorization header: %s", got)
		}
		var req openAIChatRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if req.Model != "test-model" {
			t.Fatalf("unexpected model: %s", req.Model)
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"{\"summary\":\"完成登录联调\",\"todos\":[{\"title\":\"补接口测试\",\"owner_id\":\"1001\",\"source_seq\":2}],\"risks\":[{\"level\":\"medium\",\"description\":\"Transfer 未验证\",\"source_seq\":3}]}"}}]}`))
	}))
	defer server.Close()

	provider := NewOpenAICompatibleProvider(OpenAICompatibleConfig{
		ProviderName: "openai-compatible",
		Model:        "test-model",
		BaseURL:      server.URL,
		APIKey:       "test-key",
		Timeout:      time.Second,
	})
	result, err := provider.Summarize(context.Background(), SummaryRequest{
		GroupID:        "G1",
		ConversationID: "group:G1",
		IncludeTodos:   true,
		IncludeRisks:   true,
		Messages: []Message{
			{Seq: 1, FromUID: "1001", Content: "今天完成登录联调"},
			{Seq: 2, FromUID: "1002", Content: "请补接口测试"},
			{Seq: 3, FromUID: "1003", Content: "风险是 Transfer 未验证"},
		},
	})
	if err != nil {
		t.Fatalf("Summarize error = %v", err)
	}
	if result.Summary != "完成登录联调" {
		t.Fatalf("summary = %q", result.Summary)
	}
	if len(result.Todos) != 1 || result.Todos[0].OwnerID != "1001" {
		t.Fatalf("todos = %+v", result.Todos)
	}
	if len(result.Risks) != 1 || result.Risks[0].Level != "medium" {
		t.Fatalf("risks = %+v", result.Risks)
	}
	if result.MessageStartSeq != 1 || result.MessageEndSeq != 3 {
		t.Fatalf("seq range = %d-%d", result.MessageStartSeq, result.MessageEndSeq)
	}
}

func TestFallbackProviderUsesMockWhenPrimaryFails(t *testing.T) {
	primary := NewOpenAICompatibleProvider(OpenAICompatibleConfig{
		ProviderName: "openai-compatible",
		BaseURL:      "http://127.0.0.1:1",
		Timeout:      10 * time.Millisecond,
	})
	provider := NewFallbackProvider(primary, NewMockProvider())

	result, err := provider.Summarize(context.Background(), SummaryRequest{
		GroupID:        "G1",
		ConversationID: "group:G1",
		IncludeTodos:   true,
		Messages: []Message{
			{Seq: 1, FromUID: "1001", Content: "请明天补测试"},
		},
	})
	if err != nil {
		t.Fatalf("Summarize error = %v", err)
	}
	if !strings.Contains(result.Provider, "fallback:mock") {
		t.Fatalf("provider = %s", result.Provider)
	}
	if len(result.Todos) != 1 {
		t.Fatalf("todos = %+v", result.Todos)
	}
}

func TestOpenAICompatibleProviderAnswer(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req openAIChatRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"{\"answer\":\"群聊扩散是高扇出操作，所以要用 Kafka 异步化。\",\"sources\":[{\"path\":\"docs/AI_FAQ.md\",\"title\":\"群聊为什么用 Kafka\"}]}"}}]}`))
	}))
	defer server.Close()

	provider := NewOpenAICompatibleProvider(OpenAICompatibleConfig{
		ProviderName: "openai-compatible",
		Model:        "test-model",
		BaseURL:      server.URL,
		APIKey:       "test-key",
		Timeout:      time.Second,
	})
	result, err := provider.Answer(context.Background(), AskRequest{
		Question: "群聊为什么用 Kafka？",
		Sources: []KnowledgeSource{{
			Path:    "docs/AI_FAQ.md",
			Title:   "群聊为什么用 Kafka",
			Snippet: "群聊扩散是高扇出操作，同步发送会拖慢链路。",
		}},
	})
	if err != nil {
		t.Fatalf("Answer error = %v", err)
	}
	if !strings.Contains(result.Answer, "Kafka") {
		t.Fatalf("answer = %q", result.Answer)
	}
	if len(result.Sources) != 1 || result.Sources[0].Path != "docs/AI_FAQ.md" {
		t.Fatalf("sources = %+v", result.Sources)
	}
}
