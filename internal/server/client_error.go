package server

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/1084217636/linkgo-im/api"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

const (
	clientErrorType            = "error"
	clientResultType           = "result"
	clientErrorServerBusy      = "SERVER_BUSY"
	clientErrorUnavailable     = "SERVER_UNAVAILABLE"
	clientErrorRequestCanceled = "REQUEST_CANCELED"
	clientResultAccepted       = "MESSAGE_ACCEPTED"
	clientResultRejected       = "MESSAGE_REJECTED"
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
	return writeClientDetail(writer, request, pushRejectionDetail(result))
}

func pushProcessingDetail(processErr error) clientErrorDetail {
	if processErr == nil {
		return clientErrorDetail{
			Type:    clientResultType,
			Code:    clientResultAccepted,
			Message: "message was accepted by logic for delivery",
		}
	}

	detail := clientErrorDetail{
		Type:      clientErrorType,
		Code:      clientResultRejected,
		Message:   "message processing failed",
		Retryable: isRetryablePushError(processErr),
	}
	if detail.Retryable {
		detail.Message = "message processing is temporarily unavailable; retry the same client message id"
		detail.RetryAfterMS = 250
	}
	return detail
}

func isRetryablePushError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	switch status.Code(err) {
	case codes.Aborted, codes.DeadlineExceeded, codes.ResourceExhausted, codes.Unavailable:
		return true
	default:
		return false
	}
}

func writePushResult(writer binaryWriter, request *api.WireMessage, processErr error) error {
	return writeClientDetail(writer, request, pushProcessingDetail(processErr))
}

func writeClientDetail(writer binaryWriter, request *api.WireMessage, detail clientErrorDetail) error {
	encodedDetail, err := json.Marshal(detail)
	if err != nil {
		return err
	}

	response := &api.WireMessage{
		MsgType: api.MsgType_SYSTEM,
		Body:    string(encodedDetail),
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
