package events

import (
	"encoding/json"
	"time"

	"request-sudo/internal/core"
)

const (
	TypeRequestCreated       = "request_created"
	TypeRequestApproved      = "request_approved"
	TypeRequestDenied        = "request_denied"
	TypeRequestExpired       = "request_expired"
	TypeRequestRevoked       = "request_revoked"
	TypeExecutionStarted     = "execution_started"
	TypeExecutionSucceeded   = "execution_succeeded"
	TypeExecutionFailed      = "execution_failed"
	TypeRecoveryMarkedFailed = "recovery_marked_failed"
)

type Event struct {
	EventID   string          `json:"event_id"`
	PrevHash  string          `json:"prev_hash,omitempty"`
	Hash      string          `json:"hash"`
	RequestID string          `json:"request_id"`
	Timestamp time.Time       `json:"timestamp"`
	Actor     core.Actor      `json:"actor"`
	Type      string          `json:"type"`
	Details   json.RawMessage `json:"details,omitempty"`
}

type RequestCreatedDetails struct {
	Request core.Request `json:"request"`
}

type ApprovalDetails struct {
	Approver core.Actor `json:"approver"`
	TOTP     string     `json:"totp,omitempty"`
}

type DenialDetails struct {
	Approver core.Actor `json:"approver"`
	Reason   string     `json:"reason,omitempty"`
}

type ExecutionStartedDetails struct {
	Executor string `json:"executor"`
	Digest   string `json:"digest"`
}

type ExecutionFinishedDetails struct {
	Result core.ExecutionResult `json:"result"`
}

type RecoveryMarkedFailedDetails struct {
	Message string `json:"message"`
}

func MarshalDetails(v any) (json.RawMessage, error) {
	if v == nil {
		return nil, nil
	}
	payload, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(payload), nil
}
