package svc

import (
	"context"
	"testing"

	"google.golang.org/grpc/connectivity"
)

type fakeLogicConnection struct {
	state     connectivity.State
	connected bool
}

func (f *fakeLogicConnection) Connect()                     { f.connected = true }
func (f *fakeLogicConnection) GetState() connectivity.State { return f.state }
func (f *fakeLogicConnection) WaitForStateChange(context.Context, connectivity.State) bool {
	return false
}

func TestWaitForLogicReadyAcceptsReadyConnection(t *testing.T) {
	conn := &fakeLogicConnection{state: connectivity.Ready}
	if err := waitForLogicReady(context.Background(), conn); err != nil {
		t.Fatalf("waitForLogicReady() error = %v", err)
	}
	if !conn.connected {
		t.Fatal("connection was not activated")
	}
}

func TestWaitForLogicReadyRejectsUnavailableConnection(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := waitForLogicReady(ctx, &fakeLogicConnection{state: connectivity.TransientFailure})
	if err == nil {
		t.Fatal("waitForLogicReady() error = nil")
	}
}
