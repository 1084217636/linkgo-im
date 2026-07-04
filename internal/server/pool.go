package server

import (
	"context"

	"github.com/1084217636/linkgo-im/api"
	"github.com/zeromicro/go-zero/core/logx"
)

type pushTask struct {
	ctx       context.Context
	uid       string
	data      []byte
	logic     api.LogicClient
	frame     *api.WireMessage
	gatewayID string
}

type PushWorkerPool struct {
	tasks chan pushTask
}

func NewPushWorkerPool(workerCount, queueSize int) *PushWorkerPool {
	pool := &PushWorkerPool{
		tasks: make(chan pushTask, queueSize),
	}

	for i := 0; i < workerCount; i++ {
		go func() {
			for task := range pool.tasks {
				if _, err := task.logic.PushMessage(task.ctx, &api.PushMsgReq{
					UserId:  task.uid,
					Content: task.data,
				}); err != nil {
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
			}
		}()
	}

	return pool
}

func (p *PushWorkerPool) Submit(ctx context.Context, uid string, logic api.LogicClient, data []byte, frame *api.WireMessage, gatewayID string) bool {
	select {
	case p.tasks <- pushTask{ctx: ctx, uid: uid, logic: logic, data: data, frame: frame, gatewayID: gatewayID}:
		return true
	default:
		return false
	}
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
