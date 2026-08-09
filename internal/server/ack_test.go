package server

import (
	"context"
	"encoding/base64"
	"testing"

	"github.com/1084217636/linkgo-im/api"
	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"google.golang.org/protobuf/proto"
)

func TestAckMessageAtomicallyClearsDeliveryState(t *testing.T) {
	redisServer := miniredis.RunT(t)
	ctx := context.Background()
	rdb := redis.NewClient(&redis.Options{Addr: redisServer.Addr()})
	defer rdb.Close()

	const (
		uid       = "1002"
		messageID = "c2c:1001:1002-7"
	)
	payload, err := proto.Marshal(&api.WireMessage{
		MessageId: messageID,
		SessionId: "c2c:1001:1002",
		Seq:       7,
	})
	if err != nil {
		t.Fatalf("proto.Marshal error = %v", err)
	}
	encoded := base64.StdEncoding.EncodeToString(payload)
	if err := rdb.ZAdd(ctx, PendingAckKey(uid), redis.Z{Score: 1, Member: messageID}).Err(); err != nil {
		t.Fatalf("seed pending ack: %v", err)
	}
	if err := rdb.ZAdd(ctx, OfflineMessageKey(uid), redis.Z{Score: 1, Member: messageID}).Err(); err != nil {
		t.Fatalf("seed offline message: %v", err)
	}
	if err := rdb.HSet(ctx, AckIndexKey(uid), messageID, encoded).Err(); err != nil {
		t.Fatalf("seed ack index: %v", err)
	}
	if err := rdb.HSet(ctx, AckRetryKey(uid), messageID, 2).Err(); err != nil {
		t.Fatalf("seed retry state: %v", err)
	}

	AckMessage(ctx, rdb, uid, messageID)

	assertSortedSetMemberMissing(t, ctx, rdb, PendingAckKey(uid), messageID)
	assertSortedSetMemberMissing(t, ctx, rdb, OfflineMessageKey(uid), messageID)
	for _, key := range []string{AckIndexKey(uid), AckRetryKey(uid)} {
		exists, err := rdb.HExists(ctx, key, messageID).Result()
		if err != nil {
			t.Fatalf("HExists(%s) error = %v", key, err)
		}
		if exists {
			t.Fatalf("%s still contains %s", key, messageID)
		}
	}
	readSeq, err := rdb.HGet(ctx, UserConversationAckedKey(uid), "c2c:1001:1002").Int64()
	if err != nil {
		t.Fatalf("acked conversation seq: %v", err)
	}
	if readSeq != 7 {
		t.Fatalf("acked seq = %d, want 7", readSeq)
		if _, err := rdb.HGet(ctx, UserConversationReadKey(uid), "c2c:1001:1002").Result(); err != redis.Nil {
			t.Fatalf("ACK should not advance read_seq, error = %v", err)
		}
	}

	// Repeated ACKs are harmless and cannot recreate partially deleted state.
	AckMessage(ctx, rdb, uid, messageID)
}

func assertSortedSetMemberMissing(t *testing.T, ctx context.Context, rdb *redis.Client, key, member string) {
	t.Helper()
	if _, err := rdb.ZScore(ctx, key, member).Result(); err != redis.Nil {
		t.Fatalf("ZScore(%s, %s) error = %v, want redis.Nil", key, member, err)
	}
}
