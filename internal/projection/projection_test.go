package projection_test

import (
	"testing"
	"time"

	"request-sudo/internal/core"
	"request-sudo/internal/events"
	"request-sudo/internal/projection"
)

func TestProjectionAppliesHappyPath(t *testing.T) {
	createdDetails, _ := events.MarshalDetails(events.RequestCreatedDetails{Request: core.Request{ID: "req_1", CreatedAt: time.Now().UTC(), ExpiresAt: time.Now().UTC().Add(time.Minute)}})
	approvedDetails, _ := events.MarshalDetails(events.ApprovalDetails{Approver: core.Actor{Kind: "local", ID: "root"}})
	finishedDetails, _ := events.MarshalDetails(events.ExecutionFinishedDetails{Result: core.ExecutionResult{ExitCode: 0}})
	history := []events.Event{
		{RequestID: "req_1", Type: events.TypeRequestCreated, Details: createdDetails},
		{RequestID: "req_1", Type: events.TypeRequestApproved, Details: approvedDetails},
		{RequestID: "req_1", Type: events.TypeExecutionStarted},
		{RequestID: "req_1", Type: events.TypeExecutionSucceeded, Details: finishedDetails},
	}
	proj, err := projection.Rebuild(history)
	if err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	snap, ok := proj.Get("req_1")
	if !ok {
		t.Fatalf("snapshot missing")
	}
	if snap.Status != core.StatusExecuted {
		t.Fatalf("unexpected status: %s", snap.Status)
	}
	if snap.Execution == nil || snap.Execution.ExitCode != 0 {
		t.Fatalf("execution result missing")
	}
}

func TestProjectionRejectsForbiddenTransition(t *testing.T) {
	proj := projection.New()
	createdDetails, _ := events.MarshalDetails(events.RequestCreatedDetails{Request: core.Request{ID: "req_1", CreatedAt: time.Now().UTC(), ExpiresAt: time.Now().UTC().Add(time.Minute)}})
	if err := proj.Apply(events.Event{RequestID: "req_1", Type: events.TypeRequestCreated, Details: createdDetails}); err != nil {
		t.Fatalf("apply create: %v", err)
	}
	if err := proj.Apply(events.Event{RequestID: "req_1", Type: events.TypeExecutionStarted}); err == nil {
		t.Fatalf("expected forbidden transition error")
	}
}
