package ai

import (
	"context"
	"fmt"
)

type FallbackProvider struct {
	primary  Provider
	fallback Provider
}

func NewFallbackProvider(primary, fallback Provider) *FallbackProvider {
	if fallback == nil {
		fallback = NewMockProvider()
	}
	return &FallbackProvider{primary: primary, fallback: fallback}
}

func (p *FallbackProvider) Name() string {
	if p == nil {
		return mockProviderName
	}
	if p.primary == nil {
		if p.fallback == nil {
			return mockProviderName
		}
		return p.fallback.Name()
	}
	return p.primary.Name()
}

func (p *FallbackProvider) Summarize(ctx context.Context, req SummaryRequest) (*SummaryResult, error) {
	if p == nil {
		return NewMockProvider().Summarize(ctx, req)
	}
	if p.primary == nil {
		if p.fallback == nil {
			return NewMockProvider().Summarize(ctx, req)
		}
		return p.fallback.Summarize(ctx, req)
	}
	result, err := p.primary.Summarize(ctx, req)
	if err == nil {
		return result, nil
	}
	fallbackResult, fallbackErr := p.fallback.Summarize(ctx, req)
	if fallbackErr != nil {
		return nil, fmt.Errorf("%s failed: %w; fallback failed: %v", p.primary.Name(), err, fallbackErr)
	}
	fallbackResult.Provider = p.primary.Name() + ":fallback:" + p.fallback.Name()
	return fallbackResult, nil
}
