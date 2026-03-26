package server

import (
	"context"
	"log"

	"github.com/1084217636/linkgo-im/api"
)

type pushTask struct {
	uid   string
	data  []byte
	logic api.LogicClient
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
				if _, err := task.logic.PushMessage(context.Background(), &api.PushMsgReq{
					UserId:  task.uid,
					Content: task.data,
				}); err != nil {
					log.Printf("push message failed for user=%s: %v", task.uid, err)
				}
			}
		}()
	}

	return pool
}

func (p *PushWorkerPool) Submit(uid string, logic api.LogicClient, data []byte) bool {
	select {
	case p.tasks <- pushTask{uid: uid, logic: logic, data: data}:
		return true
	default:
		return false
	}
}
