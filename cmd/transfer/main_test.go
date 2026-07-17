package main

import (
	"context"
	"errors"
	"sync"
	"testing"

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
