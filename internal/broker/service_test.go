package broker_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"lease-broker-successor/internal/broker"
	"lease-broker-successor/internal/core"
	"lease-broker-successor/internal/events"
)

type fakeExecutor struct{}

func (fakeExecutor) Name() string { return "fake" }

func (fakeExecutor) Execute(ctx context.Context, req core.Request) (core.ExecutionResult, error) {
	return core.ExecutionResult{StartedAt: time.Now().UTC(), FinishedAt: time.Now().UTC(), ExitCode: 0, Stdout: "ok\n"}, nil
}

func TestServiceSubmitApproveExecuteAndReplay(t *testing.T) {
	log, err := events.NewLog(filepath.Join(t.TempDir(), "events.jsonl"))
	if err != nil {
		t.Fatalf("new log: %v", err)
	}
	svc, err := broker.NewService(log, fakeExecutor{}, broker.WithHostname("test-host"))
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	ctx := context.Background()
	submit, err := svc.Submit(ctx, broker.SubmitInput{Argv: []string{"/bin/echo", "hello"}, Reason: "smoke path", Mode: core.ModePoll, Requester: core.PeerIdentity{UID: 1001, Username: "alice"}, Cwd: "/tmp"})
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	approve, err := svc.Approve(ctx, broker.ReviewInput{RequestID: submit.RequestID, Approver: core.Actor{Kind: "local", ID: "root"}, TOTP: "123456"})
	if err != nil {
		t.Fatalf("approve: %v", err)
	}
	if approve.Status != string(core.StatusApproved) {
		t.Fatalf("unexpected approve status: %s", approve.Status)
	}
	execute, err := svc.Execute(ctx, submit.RequestID)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if execute.Status != string(core.StatusExecuted) {
		t.Fatalf("unexpected execute status: %s", execute.Status)
	}
	if execute.ExitCode == nil || *execute.ExitCode != 0 {
		t.Fatalf("unexpected exit code: %#v", execute.ExitCode)
	}
	replayed, err := broker.NewService(log, fakeExecutor{}, broker.WithHostname("test-host"))
	if err != nil {
		t.Fatalf("rebuild service: %v", err)
	}
	status, err := replayed.Status(ctx, submit.RequestID)
	if err != nil {
		t.Fatalf("status after replay: %v", err)
	}
	if status.Status != string(core.StatusExecuted) {
		t.Fatalf("unexpected replayed status: %s", status.Status)
	}
	secondExecute, err := replayed.Execute(ctx, submit.RequestID)
	if err != nil {
		t.Fatalf("second execute: %v", err)
	}
	if secondExecute.Status != string(core.StatusExecuted) {
		t.Fatalf("expected idempotent executed status, got %s", secondExecute.Status)
	}
}
