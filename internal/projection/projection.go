package projection

import (
	"encoding/json"
	"fmt"
	"sort"

	"request-sudo/internal/core"
	"request-sudo/internal/events"
)

type Projection struct {
	requests map[string]core.Snapshot
}

func New() *Projection {
	return &Projection{requests: make(map[string]core.Snapshot)}
}

func Rebuild(history []events.Event) (*Projection, error) {
	p := New()
	for _, event := range history {
		if err := p.Apply(event); err != nil {
			return nil, err
		}
	}
	return p, nil
}

func (p *Projection) Apply(event events.Event) error {
	snap, ok := p.requests[event.RequestID]
	switch event.Type {
	case events.TypeRequestCreated:
		if ok {
			return fmt.Errorf("request %s already exists", event.RequestID)
		}
		var details events.RequestCreatedDetails
		if err := json.Unmarshal(event.Details, &details); err != nil {
			return err
		}
		p.requests[event.RequestID] = core.Snapshot{
			Request:       details.Request,
			Status:        core.StatusPending,
			LastEventType: event.Type,
			LastEventAt:   event.Timestamp,
			LastEventHash: event.Hash,
		}
		return nil
	}
	if !ok {
		return fmt.Errorf("request %s not found for event %s", event.RequestID, event.Type)
	}

	switch event.Type {
	case events.TypeRequestApproved:
		if snap.Status != core.StatusPending {
			return fmt.Errorf("approve invalid from %s", snap.Status)
		}
		var details events.ApprovalDetails
		if err := json.Unmarshal(event.Details, &details); err != nil {
			return err
		}
		snap.Status = core.StatusApproved
		snap.Approver = &details.Approver
	case events.TypeRequestDenied:
		if snap.Status != core.StatusPending {
			return fmt.Errorf("deny invalid from %s", snap.Status)
		}
		var details events.DenialDetails
		if err := json.Unmarshal(event.Details, &details); err != nil {
			return err
		}
		snap.Status = core.StatusDenied
		snap.Approver = &details.Approver
		snap.DenyReason = details.Reason
	case events.TypeRequestExpired:
		if snap.Status != core.StatusPending {
			return fmt.Errorf("expire invalid from %s", snap.Status)
		}
		snap.Status = core.StatusExpired
	case events.TypeRequestRevoked:
		if snap.Status != core.StatusPending && snap.Status != core.StatusApproved {
			return fmt.Errorf("revoke invalid from %s", snap.Status)
		}
		snap.Status = core.StatusRevoked
	case events.TypeExecutionStarted:
		if snap.Status != core.StatusApproved {
			return fmt.Errorf("execution start invalid from %s", snap.Status)
		}
		snap.Status = core.StatusExecuting
	case events.TypeExecutionSucceeded:
		if snap.Status != core.StatusExecuting {
			return fmt.Errorf("execution success invalid from %s", snap.Status)
		}
		var details events.ExecutionFinishedDetails
		if err := json.Unmarshal(event.Details, &details); err != nil {
			return err
		}
		snap.Status = core.StatusExecuted
		snap.Execution = &details.Result
	case events.TypeExecutionFailed, events.TypeRecoveryMarkedFailed:
		if snap.Status != core.StatusExecuting {
			return fmt.Errorf("execution failure invalid from %s", snap.Status)
		}
		if len(event.Details) > 0 && event.Type == events.TypeExecutionFailed {
			var details events.ExecutionFinishedDetails
			if err := json.Unmarshal(event.Details, &details); err != nil {
				return err
			}
			snap.Execution = &details.Result
		}
		snap.Status = core.StatusFailed
	default:
		return fmt.Errorf("unsupported event type %s", event.Type)
	}

	snap.LastEventType = event.Type
	snap.LastEventAt = event.Timestamp
	snap.LastEventHash = event.Hash
	p.requests[event.RequestID] = snap
	return nil
}

func (p *Projection) Get(requestID string) (core.Snapshot, bool) {
	snap, ok := p.requests[requestID]
	return snap, ok
}

func (p *Projection) All() []core.Snapshot {
	keys := make([]string, 0, len(p.requests))
	for key := range p.requests {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make([]core.Snapshot, 0, len(keys))
	for _, key := range keys {
		out = append(out, p.requests[key])
	}
	return out
}
