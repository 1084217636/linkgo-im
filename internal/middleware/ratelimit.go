package middleware

import (
	"hash/fnv"
	"sync"
	"time"
)

type tokenBucket struct {
	tokens     float64
	lastRefill time.Time
	lastSeen   time.Time
}

type limiterShard struct {
	mu        sync.Mutex
	buckets   map[string]*tokenBucket
	lastSweep time.Time
}

type TokenBucketLimiter struct {
	rate     float64
	capacity float64
	shards   []limiterShard
}

const (
	limiterShardCount = 32
	limiterBucketTTL  = 10 * time.Minute
	limiterSweepEvery = time.Minute
)

func NewTokenBucketLimiter(rate float64, capacity int) *TokenBucketLimiter {
	l := &TokenBucketLimiter{
		rate:     rate,
		capacity: float64(capacity),
		shards:   make([]limiterShard, limiterShardCount),
	}
	for i := range l.shards {
		l.shards[i].buckets = make(map[string]*tokenBucket)
	}
	return l
}

func (l *TokenBucketLimiter) Allow(key string) bool {
	if l == nil {
		return true
	}
	if key == "" {
		key = "anonymous"
	}
	now := time.Now()
	shard := l.shardFor(key)

	shard.mu.Lock()
	defer shard.mu.Unlock()

	l.sweepLocked(shard, now)

	bucket, ok := shard.buckets[key]
	if !ok {
		shard.buckets[key] = &tokenBucket{
			tokens:     l.capacity - 1,
			lastRefill: now,
			lastSeen:   now,
		}
		return true
	}

	elapsed := now.Sub(bucket.lastRefill).Seconds()
	bucket.tokens = minFloat(l.capacity, bucket.tokens+elapsed*l.rate)
	bucket.lastRefill = now
	bucket.lastSeen = now

	if bucket.tokens < 1 {
		return false
	}

	bucket.tokens--
	return true
}

func (l *TokenBucketLimiter) shardFor(key string) *limiterShard {
	if len(l.shards) == 0 {
		l.shards = make([]limiterShard, limiterShardCount)
		for i := range l.shards {
			l.shards[i].buckets = make(map[string]*tokenBucket)
		}
	}
	h := fnv.New32a()
	_, _ = h.Write([]byte(key))
	return &l.shards[int(h.Sum32())%len(l.shards)]
}

func (l *TokenBucketLimiter) sweepLocked(shard *limiterShard, now time.Time) {
	if shard == nil || now.Sub(shard.lastSweep) < limiterSweepEvery {
		return
	}
	shard.lastSweep = now
	for key, bucket := range shard.buckets {
		if now.Sub(bucket.lastSeen) > limiterBucketTTL {
			delete(shard.buckets, key)
		}
	}
}

func minFloat(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}
