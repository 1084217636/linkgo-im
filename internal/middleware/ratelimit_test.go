package middleware

import (
	"sync"
	"testing"
)

func TestTokenBucketLimiterAllowsCapacityThenLimits(t *testing.T) {
	limiter := NewTokenBucketLimiter(0, 2)
	if !limiter.Allow("u1") {
		t.Fatal("first request was limited")
	}
	if !limiter.Allow("u1") {
		t.Fatal("second request was limited")
	}
	if limiter.Allow("u1") {
		t.Fatal("third request should be limited")
	}
}

func TestTokenBucketLimiterConcurrentDifferentKeys(t *testing.T) {
	limiter := NewTokenBucketLimiter(100, 10)
	var wg sync.WaitGroup
	errs := make(chan string, 128)

	for i := 0; i < 128; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			key := string(rune('a'+(i%26))) + string(rune('0'+(i%10)))
			if !limiter.Allow(key) {
				errs <- key
			}
		}()
	}
	wg.Wait()
	close(errs)
	if len(errs) > 0 {
		t.Fatalf("unexpected limited keys: %d", len(errs))
	}
}
