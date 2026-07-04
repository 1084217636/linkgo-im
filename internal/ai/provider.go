package ai

import (
	"context"
	"strings"
)

type Provider interface {
	Name() string
	Summarize(ctx context.Context, req SummaryRequest) (*SummaryResult, error)
}

func NewProvider(name string) Provider {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "", "mock":
		return NewMockProvider()
	default:
		return NewMockProvider()
	}
}
