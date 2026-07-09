package ai

import (
	"context"
	"strings"
	"time"
)

type Provider interface {
	Name() string
	Summarize(ctx context.Context, req SummaryRequest) (*SummaryResult, error)
	Answer(ctx context.Context, req AskRequest) (*AskResult, error)
}

type ProviderOptions struct {
	Name           string
	Model          string
	BaseURL        string
	APIKey         string
	Timeout        time.Duration
	FallbackToMock bool
}

func NewProvider(name string) Provider {
	return NewProviderWithOptions(ProviderOptions{Name: name, FallbackToMock: true})
}

func NewProviderWithOptions(opts ProviderOptions) Provider {
	name := strings.ToLower(strings.TrimSpace(opts.Name))
	if opts.Timeout <= 0 {
		opts.Timeout = 10 * time.Second
	}
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "", "mock":
		return NewMockProvider()
	case "openai", "openai-compatible", "siliconflow", "deepseek":
		if name == "deepseek" {
			if strings.TrimSpace(opts.BaseURL) == "" {
				opts.BaseURL = "https://api.deepseek.com"
			}
			if strings.TrimSpace(opts.Model) == "" {
				opts.Model = "deepseek-v4-flash"
			}
		}
		primary := NewOpenAICompatibleProvider(OpenAICompatibleConfig{
			ProviderName: name,
			Model:        opts.Model,
			BaseURL:      opts.BaseURL,
			APIKey:       opts.APIKey,
			Timeout:      opts.Timeout,
		})
		if opts.FallbackToMock {
			return NewFallbackProvider(primary, NewMockProvider())
		}
		return primary
	default:
		return NewMockProvider()
	}
}
