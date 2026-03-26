package svc

import (
	"context"
	"encoding/json"

	"github.com/1084217636/linkgo-im/api"
	"github.com/segmentio/kafka-go"
)

type groupDispatchJob struct {
	Frame      *api.WireMessage `json:"frame"`
	Recipients []string         `json:"recipients"`
	Attempt    int              `json:"attempt"`
}

type kafkaDispatcher struct {
	writer *kafka.Writer
}

func (d *kafkaDispatcher) PublishGroupDispatch(ctx context.Context, frame *api.WireMessage, recipients []string) error {
	payload, err := json.Marshal(groupDispatchJob{
		Frame:      frame,
		Recipients: recipients,
		Attempt:    0,
	})
	if err != nil {
		return err
	}

	return d.writer.WriteMessages(ctx, kafka.Message{
		Key:   []byte(frame.SessionId),
		Value: payload,
	})
}
