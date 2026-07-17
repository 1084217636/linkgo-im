package server

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/1084217636/linkgo-im/api"
	"google.golang.org/protobuf/proto"
)

type recordingBinaryWriter struct {
	payload []byte
	err     error
}

func (w *recordingBinaryWriter) WriteBinary(payload []byte) error {
	w.payload = append([]byte(nil), payload...)
	return w.err
}

func TestWritePushRejectionQueueFull(t *testing.T) {
	writer := &recordingBinaryWriter{}
	request := &api.WireMessage{
		ClientMsgId: "cmid-1",
		TraceId:     "trace-1",
	}

	if err := writePushRejection(writer, request, SubmitQueueFull); err != nil {
		t.Fatalf("writePushRejection() error = %v", err)
	}

	var frame api.WireMessage
	if err := proto.Unmarshal(writer.payload, &frame); err != nil {
		t.Fatalf("unmarshal rejection: %v", err)
	}
	if frame.MsgType != api.MsgType_SYSTEM {
		t.Fatalf("MsgType = %s, want SYSTEM", frame.MsgType)
	}
	if frame.ClientMsgId != request.ClientMsgId || frame.TraceId != request.TraceId {
		t.Fatalf("correlation fields = (%q, %q)", frame.ClientMsgId, frame.TraceId)
	}

	var detail clientErrorDetail
	if err := json.Unmarshal([]byte(frame.Body), &detail); err != nil {
		t.Fatalf("unmarshal error detail: %v", err)
	}
	if detail.Type != clientErrorType || detail.Code != clientErrorServerBusy {
		t.Fatalf("error detail = %#v", detail)
	}
	if !detail.Retryable || detail.RetryAfterMS <= 0 {
		t.Fatalf("retry contract = %#v", detail)
	}
}

func TestPushRejectionDetailMappings(t *testing.T) {
	tests := []struct {
		name      string
		result    SubmitResult
		code      string
		retryable bool
	}{
		{name: "queue full", result: SubmitQueueFull, code: clientErrorServerBusy, retryable: true},
		{name: "pool closed", result: SubmitPoolClosed, code: clientErrorUnavailable, retryable: true},
		{name: "context canceled", result: SubmitContextCanceled, code: clientErrorRequestCanceled, retryable: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			detail := pushRejectionDetail(tt.result)
			if detail.Code != tt.code || detail.Retryable != tt.retryable {
				t.Fatalf("pushRejectionDetail(%s) = %#v", tt.result, detail)
			}
		})
	}
}

func TestWritePushRejectionPropagatesWriteError(t *testing.T) {
	want := errors.New("socket closed")
	writer := &recordingBinaryWriter{err: want}
	if err := writePushRejection(writer, &api.WireMessage{}, SubmitQueueFull); !errors.Is(err, want) {
		t.Fatalf("writePushRejection() error = %v, want %v", err, want)
	}
}
