package server

import (
	"context"
	"errors"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/1084217636/linkgo-im/internal/metrics"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestPushWorkerPoolPreservesOrderForSameUID(t *testing.T) {
	var (
		mu        sync.Mutex
		processed []int
	)
	pool := newPushWorkerPool(4, 128, func(task pushTask) error {
		value, err := strconv.Atoi(string(task.data))
		if err != nil {
			return err
		}
		mu.Lock()
		processed = append(processed, value)
		mu.Unlock()
		return nil
	})

	for i := 0; i < 100; i++ {
		result := pool.Submit(context.Background(), "user-ordered", nil, []byte(strconv.Itoa(i)), nil, "gateway-test")
		if result != SubmitAccepted {
			t.Fatalf("Submit(%d) = %s, want %s", i, result, SubmitAccepted)
		}
	}
	if err := pool.Close(context.Background()); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(processed) != 100 {
		t.Fatalf("processed count = %d, want 100", len(processed))
	}
	for i, value := range processed {
		if value != i {
			t.Fatalf("processed[%d] = %d, want %d", i, value, i)
		}
	}
}

func TestPushWorkerPoolRunsDifferentShardsInParallel(t *testing.T) {
	const shardCount = 4
	uidA := "user-a"
	uidB := uidOnDifferentShard(uidA, shardCount)
	started := make(chan string, 2)
	release := make(chan struct{})
	pool := newPushWorkerPool(shardCount, 2, func(task pushTask) error {
		started <- task.uid
		<-release
		return nil
	})

	if result := pool.Submit(context.Background(), uidA, nil, nil, nil, "gateway-test"); result != SubmitAccepted {
		t.Fatalf("first Submit() = %s", result)
	}
	if result := pool.Submit(context.Background(), uidB, nil, nil, nil, "gateway-test"); result != SubmitAccepted {
		t.Fatalf("second Submit() = %s", result)
	}

	seen := map[string]bool{}
	for len(seen) < 2 {
		select {
		case uid := <-started:
			seen[uid] = true
		case <-time.After(time.Second):
			close(release)
			_ = pool.Close(context.Background())
			t.Fatalf("tasks did not run in parallel; started = %#v", seen)
		}
	}
	close(release)
	if err := pool.Close(context.Background()); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
}

func TestPushWorkerPoolReportsQueueFull(t *testing.T) {
	started := make(chan struct{}, 1)
	release := make(chan struct{})
	pool := newPushWorkerPool(1, 1, func(task pushTask) error {
		started <- struct{}{}
		<-release
		return nil
	})

	if result := pool.Submit(context.Background(), "user-a", nil, nil, nil, "gateway-test"); result != SubmitAccepted {
		t.Fatalf("first Submit() = %s", result)
	}
	<-started
	if result := pool.Submit(context.Background(), "user-a", nil, nil, nil, "gateway-test"); result != SubmitAccepted {
		t.Fatalf("second Submit() = %s", result)
	}
	before := testutil.ToFloat64(metrics.PushQueueSubmissions.WithLabelValues(string(SubmitQueueFull)))
	if result := pool.Submit(context.Background(), "user-a", nil, nil, nil, "gateway-test"); result != SubmitQueueFull {
		t.Fatalf("third Submit() = %s, want %s", result, SubmitQueueFull)
	}
	after := testutil.ToFloat64(metrics.PushQueueSubmissions.WithLabelValues(string(SubmitQueueFull)))
	if after != before+1 {
		t.Fatalf("queue_full metric delta = %v, want 1", after-before)
	}

	close(release)
	if err := pool.Close(context.Background()); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
}

func TestPushWorkerPoolRejectsCanceledAndClosedSubmissions(t *testing.T) {
	pool := newPushWorkerPool(1, 1, func(task pushTask) error { return nil })
	canceled, cancel := context.WithCancel(context.Background())
	cancel()

	if result := pool.Submit(canceled, "user-a", nil, nil, nil, "gateway-test"); result != SubmitContextCanceled {
		t.Fatalf("canceled Submit() = %s, want %s", result, SubmitContextCanceled)
	}
	if err := pool.Close(context.Background()); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if result := pool.Submit(context.Background(), "user-a", nil, nil, nil, "gateway-test"); result != SubmitPoolClosed {
		t.Fatalf("closed Submit() = %s, want %s", result, SubmitPoolClosed)
	}
}

func TestPushWorkerPoolCloseHonorsContextTimeout(t *testing.T) {
	started := make(chan struct{}, 1)
	release := make(chan struct{})
	pool := newPushWorkerPool(1, 1, func(task pushTask) error {
		started <- struct{}{}
		<-release
		return nil
	})
	if result := pool.Submit(context.Background(), "user-a", nil, nil, nil, "gateway-test"); result != SubmitAccepted {
		t.Fatalf("Submit() = %s", result)
	}
	<-started

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if err := pool.Close(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Close() error = %v, want deadline exceeded", err)
	}
	close(release)
	if err := pool.Close(context.Background()); err != nil {
		t.Fatalf("second Close() error = %v", err)
	}
}

func uidOnDifferentShard(base string, shardCount int) string {
	baseShard := pushShardIndex(base, shardCount)
	for i := 0; ; i++ {
		candidate := "user-" + strconv.Itoa(i)
		if pushShardIndex(candidate, shardCount) != baseShard {
			return candidate
		}
	}
}
