package server

import (
	"encoding/json"
	"time"

	"github.com/1084217636/linkgo-im/api"
	"google.golang.org/protobuf/proto"
)

const (
	clientErrorType            = "error"
	clientErrorServerBusy      = "SERVER_BUSY"
	clientErrorUnavailable     = "SERVER_UNAVAILABLE"
	clientErrorRequestCanceled = "REQUEST_CANCELED"
)

type binaryWriter interface {
	WriteBinary(payload []byte) error
}

type clientErrorDetail struct {
	Type         string `json:"type"`
	Code         string `json:"code"`
	Message      string `json:"message"`
	Retryable    bool   `json:"retryable"`
	RetryAfterMS int64  `json:"retry_after_ms,omitempty"`
}

func pushRejectionDetail(result SubmitResult) clientErrorDetail {
	detail := clientErrorDetail{Type: clientErrorType}
	switch result {
	case SubmitQueueFull:
		detail.Code = clientErrorServerBusy
		detail.Message = "server is busy; retry the same client message id"
		detail.Retryable = true
		detail.RetryAfterMS = 250
	case SubmitPoolClosed:
		detail.Code = clientErrorUnavailable
		detail.Message = "message service is shutting down"
		detail.Retryable = true
		detail.RetryAfterMS = 1000
	default:
		detail.Code = clientErrorRequestCanceled
		detail.Message = "message request was canceled"
	}
	return detail
}

func writePushRejection(writer binaryWriter, request *api.WireMessage, result SubmitResult) error {
	detail, err := json.Marshal(pushRejectionDetail(result))
	if err != nil {
		return err
	}

	response := &api.WireMessage{
		MsgType: api.MsgType_SYSTEM,
		Body:    string(detail),
		SentAt:  time.Now().UnixMilli(),
	}
	if request != nil {
		response.ClientMsgId = request.ClientMsgId
		response.TraceId = request.TraceId
	}
	payload, err := proto.Marshal(response)
	if err != nil {
		return err
	}
	return writer.WriteBinary(payload)
}
