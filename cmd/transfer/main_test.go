package main

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/segmentio/kafka-go"
)

type fakeMessageReader struct {
	message     kafka.Message
	events      *[]string
	cancel      context.CancelFunc
	fetched     bool
	commitCalls int
}

func (r *fakeMessageReader) FetchMessage(ctx context.Context) (kafka.Message, error) {
	if r.fetched {
		<-ctx.Done()
		return kafka.Message{}, ctx.Err()
	}
	r.fetched = true
	*r.events = append(*r.events, "fetch")
	return r.message, nil
}

func (r *fakeMessageReader) CommitMessages(_ context.Context, _ ...kafka.Message) error {
	r.commitCalls++
	*r.events = append(*r.events, "commit")
	if r.cancel != nil {
		r.cancel()
	}
	return nil
}

type fakeMessageWriter struct {
	name   string
	err    error
	events *[]string
	mu     sync.Mutex
}

func (w *fakeMessageWriter) WriteMessages(_ context.Context, _ ...kafka.Message) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	*w.events = append(*w.events, w.name)
	return w.err
}

func TestGetEnv(t *testing.T) {
	t.Setenv("TRANSFER_TEST_KEY", "value")

	if got := getEnv("TRANSFER_TEST_KEY", "fallback"); got != "value" {
		t.Fatalf("getEnv existing = %q", got)
	}
	if got := getEnv("TRANSFER_TEST_MISSING", "fallback"); got != "fallback" {
		t.Fatalf("getEnv missing = %q", got)
	}
}

func TestStageLabel(t *testing.T) {
	if got := stageLabel(false); got != "consume" {
		t.Fatalf("stageLabel(false) = %q", got)
	}
	if got := stageLabel(true); got != "retry_consume" {
		t.Fatalf("stageLabel(true) = %q", got)
	}
}

func TestGetEnvInt(t *testing.T) {
	t.Setenv("TRANSFER_INT", "5")
	t.Setenv("TRANSFER_BAD_INT", "bad")

	if got := getEnvInt("TRANSFER_INT", 3); got != 5 {
		t.Fatalf("getEnvInt existing = %d, want 5", got)
	}
	if got := getEnvInt("TRANSFER_BAD_INT", 3); got != 3 {
		t.Fatalf("getEnvInt bad = %d, want fallback", got)
	}
	if got := getEnvInt("TRANSFER_MISSING_INT", 3); got != 3 {
		t.Fatalf("getEnvInt missing = %d, want fallback", got)
	}
}

func TestGroupRecipientDedupKey(t *testing.T) {
	if got := groupRecipientDedupKey("msg-1", "1002"); got != "group_delivery:msg-1:1002" {
		t.Fatalf("groupRecipientDedupKey = %q", got)
	}
}

func TestConsumeLoopCommitsOnlyAfterMalformedMessageReachesDLQ(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	events := []string{}
	reader := &fakeMessageReader{
		message: kafka.Message{Key: []byte("bad"), Value: []byte("not-json")},
		events:  &events,
		cancel:  cancel,
	}
	retryWriter := &fakeMessageWriter{name: "retry", events: &events}
	dlqWriter := &fakeMessageWriter{name: "dlq", events: &events}

	consumeLoop(ctx, reader, retryWriter, dlqWriter, nil, nil, false, 3)

	want := []string{"fetch", "dlq", "commit"}
	if len(events) != len(want) {
		t.Fatalf("events = %v, want %v", events, want)
	}
	for i := range want {
		if events[i] != want[i] {
			t.Fatalf("events = %v, want %v", events, want)
		}
	}
}

func TestProcessFetchedMessageDoesNotSucceedWhenDLQPublishFails(t *testing.T) {
	events := []string{}
	dlqErr := errors.New("dlq unavailable")
	dlqWriter := &fakeMessageWriter{name: "dlq", events: &events, err: dlqErr}

	err := processFetchedMessage(
		context.Background(),
		kafka.Message{Value: []byte("not-json")},
		&fakeMessageWriter{name: "retry", events: &events},
		dlqWriter,
		nil,
		nil,
		false,
		3,
	)
	if !errors.Is(err, dlqErr) {
		t.Fatalf("processFetchedMessage() error = %v, want %v", err, dlqErr)
	}
}

func TestRecipientLeaseLifecycle(t *testing.T) {
	srv := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: srv.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	ctx := context.Background()
	key := groupRecipientDedupKey("msg-lease", "1002")

	result, err := claimGroupRecipient(ctx, rdb, key, "owner-a", time.Minute)
	if err != nil || result != recipientClaimed {
		t.Fatalf("first claim = (%s, %v)", result, err)
	}
	if value, _ := srv.Get(key); value != "processing:owner-a" {
		t.Fatalf("processing value = %q", value)
	}
	if ttl := srv.TTL(key); ttl <= 0 || ttl > time.Minute {
		t.Fatalf("processing TTL = %v", ttl)
	}

	result, err = claimGroupRecipient(ctx, rdb, key, "owner-b", time.Minute)
	if err != nil || result != recipientBusy {
		t.Fatalf("competing claim = (%s, %v)", result, err)
	}
	if completed, err := completeGroupRecipient(ctx, rdb, key, "owner-b", time.Hour); err != nil || completed {
		t.Fatalf("wrong owner completion = (%v, %v)", completed, err)
	}
	if completed, err := completeGroupRecipient(ctx, rdb, key, "owner-a", time.Hour); err != nil || !completed {
		t.Fatalf("owner completion = (%v, %v)", completed, err)
	}

	result, err = claimGroupRecipient(ctx, rdb, key, "owner-b", time.Minute)
	if err != nil || result != recipientDone {
		t.Fatalf("claim after done = (%s, %v)", result, err)
	}
}

func TestRecipientLeaseCanBeReclaimedAfterExpiry(t *testing.T) {
	srv := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: srv.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	ctx := context.Background()
	key := groupRecipientDedupKey("msg-expired", "1003")

	if result, err := claimGroupRecipient(ctx, rdb, key, "owner-a", time.Second); err != nil || result != recipientClaimed {
		t.Fatalf("first claim = (%s, %v)", result, err)
	}
	srv.FastForward(2 * time.Second)
	if result, err := claimGroupRecipient(ctx, rdb, key, "owner-b", time.Minute); err != nil || result != recipientClaimed {
		t.Fatalf("reclaim = (%s, %v)", result, err)
	}
}
