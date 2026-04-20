package socket_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"request-sudo/internal/broker"
	"request-sudo/internal/core"
	"request-sudo/internal/events"
	"request-sudo/internal/protocol"
	"request-sudo/internal/socket"
)

type fakeExecutor struct{}

func (fakeExecutor) Name() string { return "fake" }

func (fakeExecutor) Execute(ctx context.Context, req core.Request) (core.ExecutionResult, error) {
	return core.ExecutionResult{StartedAt: time.Now().UTC(), FinishedAt: time.Now().UTC(), ExitCode: 0, Stdout: "socket-ok\n"}, nil
}

func TestSocketRoundTrip(t *testing.T) {
	temp := t.TempDir()
	log, err := events.NewLog(filepath.Join(temp, "events.jsonl"))
	if err != nil {
		t.Fatalf("new log: %v", err)
	}
	svc, err := broker.NewService(log, fakeExecutor{}, broker.WithHostname("socket-host"))
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	requestSock := filepath.Join(temp, "request.sock")
	reviewSock := filepath.Join(temp, "review.sock")
	server := socket.NewServer(svc, requestSock, reviewSock, []uint32{999999}, []uint32{uint32(os.Getegid())})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		_ = server.Run(ctx)
	}()
	waitForSocket(t, requestSock)
	waitForSocket(t, reviewSock)

	submit, err := socket.Call(context.Background(), requestSock, protocol.Request{Action: protocol.ActionRequestSubmit, Argv: []string{"/bin/echo", "hello"}, Reason: "socket smoke", Mode: core.ModePoll, Cwd: "/tmp"})
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	approve, err := socket.Call(context.Background(), reviewSock, protocol.Request{Action: protocol.ActionReviewApprove, RequestID: submit.RequestID, Approver: core.Actor{Kind: "local", ID: "root"}, TOTP: "654321"})
	if err != nil {
		t.Fatalf("approve: %v", err)
	}
	if approve.Status != string(core.StatusApproved) {
		t.Fatalf("unexpected approve status: %s", approve.Status)
	}
	execute, err := socket.Call(context.Background(), requestSock, protocol.Request{Action: protocol.ActionRequestExecute, RequestID: submit.RequestID})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if execute.Status != string(core.StatusExecuted) {
		t.Fatalf("unexpected execute status: %s", execute.Status)
	}
}

func waitForSocket(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("socket %s did not appear", path)
}
