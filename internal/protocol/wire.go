package protocol

import "request-sudo/internal/core"

const (
	ActionRequestSubmit  = "request.submit"
	ActionRequestStatus  = "request.status"
	ActionRequestExecute = "request.execute"
	ActionReviewApprove  = "review.approve"
	ActionReviewDeny     = "review.deny"
)

type Request struct {
	Action    string     `json:"action"`
	Argv      []string   `json:"argv,omitempty"`
	Reason    string     `json:"reason,omitempty"`
	Mode      core.Mode  `json:"mode,omitempty"`
	RequestID string     `json:"request_id,omitempty"`
	Approver  core.Actor `json:"approver,omitempty"`
	TOTP      string     `json:"totp,omitempty"`
	Cwd       string     `json:"cwd,omitempty"`
}

type Response struct {
	RequestID string                `json:"request_id,omitempty"`
	Status    string                `json:"status"`
	Message   string                `json:"message,omitempty"`
	Summary   *core.ApprovalSummary `json:"summary,omitempty"`
	ExitCode  *int                  `json:"exit_code,omitempty"`
	Stdout    string                `json:"stdout,omitempty"`
	Stderr    string                `json:"stderr,omitempty"`
	Digest    string                `json:"digest,omitempty"`
}

func FromSnapshot(snapshot core.Snapshot, message string) Response {
	resp := Response{
		RequestID: snapshot.Request.ID,
		Status:    string(snapshot.Status),
		Message:   message,
		Summary:   &snapshot.Request.Summary,
		Digest:    snapshot.Request.Digest,
	}
	if snapshot.Execution != nil {
		exitCode := snapshot.Execution.ExitCode
		resp.ExitCode = &exitCode
		resp.Stdout = snapshot.Execution.Stdout
		resp.Stderr = snapshot.Execution.Stderr
	}
	return resp
}
