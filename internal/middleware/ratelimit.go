package middleware

import (
	"sync"
	"time"
)

type tokenBucket struct {
	tokens     float64
	lastRefill time.Time
}

type TokenBucketLimiter struct {
	mu       sync.Mutex
	buckets  map[string]*tokenBucket
	rate     float64
	capacity float64
}

func NewTokenBucketLimiter(rate float64, capacity int) *TokenBucketLimiter {
	return &TokenBucketLimiter{
		buckets:  make(map[string]*tokenBucket),
		rate:     rate,
		capacity: float64(capacity),
	}
}

func (l *TokenBucketLimiter) Allow(key string) bool {
	now := time.Now()

	l.mu.Lock()
	defer l.mu.Unlock()

	bucket, ok := l.buckets[key]
	if !ok {
		l.buckets[key] = &tokenBucket{
			tokens:     l.capacity - 1,
			lastRefill: now,
		}
		return true
	}

	elapsed := now.Sub(bucket.lastRefill).Seconds()
	bucket.tokens = minFloat(l.capacity, bucket.tokens+elapsed*l.rate)
	bucket.lastRefill = now

	if bucket.tokens < 1 {
		return false
	}

	bucket.tokens--
	return true
}

func minFloat(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}
