package server

import (
	"context"
	"errors"
	"hash/fnv"
	"strconv"
	"sync"
	"time"

	"github.com/1084217636/linkgo-im/api"
	"github.com/1084217636/linkgo-im/internal/metrics"
	"github.com/zeromicro/go-zero/core/logx"
)

type SubmitResult string

const (
	SubmitAccepted        SubmitResult = "accepted"
	SubmitQueueFull       SubmitResult = "queue_full"
	SubmitPoolClosed      SubmitResult = "pool_closed"
	SubmitContextCanceled SubmitResult = "context_canceled"
)

type pushTask struct {
	ctx       context.Context
	uid       string
	data      []byte
	logic     api.LogicClient
	frame     *api.WireMessage
	gatewayID string
}

type pushTaskHandler func(pushTask) error

type PushWorkerPool struct {
	mu      sync.RWMutex
	shards  []chan pushTask
	closed  bool
	done    chan struct{}
	wg      sync.WaitGroup
	handler pushTaskHandler
}

func NewPushWorkerPool(shardCount, queueSizePerShard int) *PushWorkerPool {
	return newPushWorkerPool(shardCount, queueSizePerShard, processPushTask)
}

func newPushWorkerPool(shardCount, queueSizePerShard int, handler pushTaskHandler) *PushWorkerPool {
	if shardCount <= 0 {
		shardCount = 1
	}
	if queueSizePerShard <= 0 {
		queueSizePerShard = 1
	}
	if handler == nil {
		handler = processPushTask
	}

	pool := &PushWorkerPool{
		shards:  make([]chan pushTask, shardCount),
		done:    make(chan struct{}),
		handler: handler,
	}
	pool.wg.Add(shardCount)
	for shard := range shardCount {
		queue := make(chan pushTask, queueSizePerShard)
		pool.shards[shard] = queue
		go pool.runShard(shard, queue)
	}
	go func() {
		pool.wg.Wait()
		close(pool.done)
	}()
	return pool
}

func (p *PushWorkerPool) Submit(
	ctx context.Context,
	uid string,
	logic api.LogicClient,
	data []byte,
	frame *api.WireMessage,
	gatewayID string,
) SubmitResult {
	if ctx == nil {
		ctx = context.Background()
	}
	if ctx.Err() != nil {
		return recordPushSubmit(SubmitContextCanceled)
	}

	p.mu.RLock()
	defer p.mu.RUnlock()
	if p.closed {
		return recordPushSubmit(SubmitPoolClosed)
	}

	shard := pushShardIndex(uid, len(p.shards))
	queue := p.shards[shard]
	task := pushTask{ctx: ctx, uid: uid, logic: logic, data: data, frame: frame, gatewayID: gatewayID}
	select {
	case queue <- task:
		metrics.PushQueueDepth.WithLabelValues(strconv.Itoa(shard)).Set(float64(len(queue)))
		return recordPushSubmit(SubmitAccepted)
	case <-ctx.Done():
		return recordPushSubmit(SubmitContextCanceled)
	default:
		return recordPushSubmit(SubmitQueueFull)
	}
}

func (p *PushWorkerPool) Close(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	p.mu.Lock()
	if !p.closed {
		p.closed = true
		for _, queue := range p.shards {
			close(queue)
		}
	}
	p.mu.Unlock()

	select {
	case <-p.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (p *PushWorkerPool) runShard(shard int, queue <-chan pushTask) {
	defer p.wg.Done()
	shardLabel := strconv.Itoa(shard)
	for task := range queue {
		metrics.PushQueueDepth.WithLabelValues(shardLabel).Set(float64(len(queue)))
		startedAt := time.Now()
		if task.ctx != nil && task.ctx.Err() != nil {
			metrics.PushProcessingLatencySeconds.WithLabelValues(string(SubmitContextCanceled)).Observe(time.Since(startedAt).Seconds())
			continue
		}

		result := "success"
		if err := p.handler(task); err != nil {
			result = "failure"
			logx.Errorw("push message to logic failed",
				logx.Field("trace_id", traceID(task.frame)),
				logx.Field("message_id", messageID(task.frame)),
				logx.Field("client_msg_id", clientMsgID(task.frame)),
				logx.Field("seq", seq(task.frame)),
				logx.Field("gateway_id", task.gatewayID),
				logx.Field("target_id", targetID(task.frame)),
				logx.Field("error", err.Error()),
			)
		}
		metrics.PushProcessingLatencySeconds.WithLabelValues(result).Observe(time.Since(startedAt).Seconds())
	}
	metrics.PushQueueDepth.WithLabelValues(shardLabel).Set(0)
}

func processPushTask(task pushTask) error {
	if task.logic == nil {
		return errors.New("logic client is required")
	}
	_, err := task.logic.PushMessage(task.ctx, &api.PushMsgReq{
		UserId:  task.uid,
		Content: task.data,
	})
	return err
}

func recordPushSubmit(result SubmitResult) SubmitResult {
	metrics.PushQueueSubmissions.WithLabelValues(string(result)).Inc()
	return result
}

func pushShardIndex(uid string, shardCount int) int {
	if shardCount <= 1 {
		return 0
	}
	hash := fnv.New32a()
	_, _ = hash.Write([]byte(uid))
	return int(hash.Sum32() % uint32(shardCount))
}

func traceID(frame *api.WireMessage) string {
	if frame == nil {
		return ""
	}
	return frame.TraceId
}

func messageID(frame *api.WireMessage) string {
	if frame == nil {
		return ""
	}
	return frame.MessageId
}

func clientMsgID(frame *api.WireMessage) string {
	if frame == nil {
		return ""
	}
	return frame.ClientMsgId
}

func seq(frame *api.WireMessage) int64 {
	if frame == nil {
		return 0
	}
	return frame.Seq
}

func targetID(frame *api.WireMessage) string {
	if frame == nil {
		return ""
	}
	return frame.To
}
