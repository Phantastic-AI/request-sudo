package core

import "time"

type Mode string

const (
	ModePoll Mode = "poll"
	ModeWait Mode = "wait"
)

type Status string

const (
	StatusPending   Status = "pending"
	StatusApproved  Status = "approved"
	StatusDenied    Status = "denied"
	StatusExpired   Status = "expired"
	StatusExecuting Status = "executing"
	StatusExecuted  Status = "executed"
	StatusFailed    Status = "failed"
	StatusRevoked   Status = "revoked"
	StatusRejected  Status = "rejected"
	StatusError     Status = "error"
)

type Actor struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
}

type PeerIdentity struct {
	UID      uint32 `json:"uid"`
	GID      uint32 `json:"gid"`
	PID      int32  `json:"pid"`
	Username string `json:"username,omitempty"`
}

type ExecutionSpec struct {
	Argv []string          `json:"argv"`
	Env  map[string]string `json:"env"`
	Cwd  string            `json:"cwd"`
}

type ApprovalSummary struct {
	Requester    string `json:"requester"`
	Host         string `json:"host"`
	ExactCommand string `json:"exact_command"`
	Reason       string `json:"reason,omitempty"`
	Effect       string `json:"effect"`
	Risk         string `json:"risk"`
}

type Request struct {
	ID        string          `json:"id"`
	Argv      []string        `json:"argv"`
	Reason    string          `json:"reason,omitempty"`
	Mode      Mode            `json:"mode"`
	Requester PeerIdentity    `json:"requester"`
	Host      string          `json:"host"`
	CreatedAt time.Time       `json:"created_at"`
	ExpiresAt time.Time       `json:"expires_at"`
	Digest    string          `json:"digest"`
	Spec      ExecutionSpec   `json:"spec"`
	Summary   ApprovalSummary `json:"summary"`
}

type ExecutionResult struct {
	StartedAt  time.Time `json:"started_at"`
	FinishedAt time.Time `json:"finished_at"`
	ExitCode   int       `json:"exit_code"`
	Stdout     string    `json:"stdout,omitempty"`
	Stderr     string    `json:"stderr,omitempty"`
}

type Snapshot struct {
	Request       Request          `json:"request"`
	Status        Status           `json:"status"`
	Approver      *Actor           `json:"approver,omitempty"`
	DenyReason    string           `json:"deny_reason,omitempty"`
	Execution     *ExecutionResult `json:"execution,omitempty"`
	LastEventType string           `json:"last_event_type,omitempty"`
	LastEventAt   time.Time        `json:"last_event_at"`
	LastEventHash string           `json:"last_event_hash,omitempty"`
}

func (s Status) Terminal() bool {
	switch s {
	case StatusDenied, StatusExpired, StatusExecuted, StatusFailed, StatusRevoked:
		return true
	default:
		return false
	}
}
