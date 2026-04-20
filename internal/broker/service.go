package broker

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strconv"
	"sync"
	"time"

	"request-sudo/internal/core"
	"request-sudo/internal/events"
	"request-sudo/internal/execution"
	"request-sudo/internal/projection"
	"request-sudo/internal/protocol"
)

const DefaultTTL = 5 * time.Minute

type Option func(*Service)

type Service struct {
	mu         sync.Mutex
	log        *events.Log
	projection *projection.Projection
	executor   execution.Executor
	now        func() time.Time
	ttl        time.Duration
	hostname   string
}

type SubmitInput struct {
	Argv      []string
	Reason    string
	Mode      core.Mode
	Requester core.PeerIdentity
	Cwd       string
}

type ReviewInput struct {
	RequestID string
	Approver  core.Actor
	TOTP      string
	Reason    string
}

func WithClock(now func() time.Time) Option {
	return func(s *Service) { s.now = now }
}

func WithTTL(ttl time.Duration) Option {
	return func(s *Service) { s.ttl = ttl }
}

func WithHostname(hostname string) Option {
	return func(s *Service) { s.hostname = hostname }
}

func NewService(log *events.Log, executor execution.Executor, opts ...Option) (*Service, error) {
	hostname, err := os.Hostname()
	if err != nil {
		hostname = "localhost"
	}
	history, err := log.Replay()
	if err != nil {
		return nil, err
	}
	proj, err := projection.Rebuild(history)
	if err != nil {
		return nil, err
	}
	svc := &Service{
		log:        log,
		projection: proj,
		executor:   executor,
		now:        func() time.Time { return time.Now().UTC() },
		ttl:        DefaultTTL,
		hostname:   hostname,
	}
	for _, opt := range opts {
		opt(svc)
	}
	if err := svc.recoverExecutingLocked(); err != nil {
		return nil, err
	}
	return svc, nil
}

func (s *Service) Submit(ctx context.Context, input SubmitInput) (protocol.Response, error) {
	_ = ctx
	if len(input.Argv) == 0 {
		return protocol.Response{Status: string(core.StatusError), Message: "argv is required"}, nil
	}
	if input.Mode == "" {
		input.Mode = core.ModePoll
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	now := s.now()
	requestID := randomID("req")
	spec := execution.BuildExecutionSpec(input.Argv, input.Cwd)
	request := core.Request{
		ID:        requestID,
		Argv:      append([]string(nil), input.Argv...),
		Reason:    input.Reason,
		Mode:      input.Mode,
		Requester: input.Requester,
		Host:      s.hostname,
		CreatedAt: now,
		ExpiresAt: now.Add(s.ttl),
		Spec:      spec,
	}
	request.Digest = digestRequest(requestID, spec, input.Requester, request.ExpiresAt)
	request.Summary = core.DescribeRequest(s.hostname, input.Requester, input.Argv, input.Reason)
	details, err := events.MarshalDetails(events.RequestCreatedDetails{Request: request})
	if err != nil {
		return protocol.Response{}, err
	}
	event, err := s.log.Append(events.Event{
		RequestID: requestID,
		Actor:     core.Actor{Kind: "requester", ID: actorIDForPeer(input.Requester)},
		Type:      events.TypeRequestCreated,
		Details:   details,
	})
	if err != nil {
		return protocol.Response{}, err
	}
	if err := s.projection.Apply(event); err != nil {
		return protocol.Response{}, err
	}
	snap, _ := s.projection.Get(requestID)
	return protocol.FromSnapshot(snap, "Approval required"), nil
}

func (s *Service) Status(ctx context.Context, requestID string) (protocol.Response, error) {
	_ = ctx
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.expireIfNeededLocked(requestID); err != nil {
		return protocol.Response{}, err
	}
	snap, ok := s.projection.Get(requestID)
	if !ok {
		return protocol.Response{RequestID: requestID, Status: string(core.StatusError), Message: "request not found"}, nil
	}
	return protocol.FromSnapshot(snap, "status retrieved"), nil
}

func (s *Service) Approve(ctx context.Context, input ReviewInput) (protocol.Response, error) {
	_ = ctx
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.expireIfNeededLocked(input.RequestID); err != nil {
		return protocol.Response{}, err
	}
	snap, ok := s.projection.Get(input.RequestID)
	if !ok {
		return protocol.Response{RequestID: input.RequestID, Status: string(core.StatusError), Message: "request not found"}, nil
	}
	if snap.Status != core.StatusPending {
		return protocol.FromSnapshot(snap, "request is not pending"), nil
	}
	if err := validateApprover(input.Approver, snap.Request.Requester); err != nil {
		return protocol.Response{RequestID: input.RequestID, Status: string(core.StatusRejected), Message: err.Error()}, nil
	}
	details, err := events.MarshalDetails(events.ApprovalDetails{Approver: input.Approver, TOTP: input.TOTP})
	if err != nil {
		return protocol.Response{}, err
	}
	event, err := s.log.Append(events.Event{RequestID: input.RequestID, Actor: input.Approver, Type: events.TypeRequestApproved, Details: details})
	if err != nil {
		return protocol.Response{}, err
	}
	if err := s.projection.Apply(event); err != nil {
		return protocol.Response{}, err
	}
	snap, _ = s.projection.Get(input.RequestID)
	return protocol.FromSnapshot(snap, "request approved"), nil
}

func (s *Service) Deny(ctx context.Context, input ReviewInput) (protocol.Response, error) {
	_ = ctx
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.expireIfNeededLocked(input.RequestID); err != nil {
		return protocol.Response{}, err
	}
	snap, ok := s.projection.Get(input.RequestID)
	if !ok {
		return protocol.Response{RequestID: input.RequestID, Status: string(core.StatusError), Message: "request not found"}, nil
	}
	if snap.Status != core.StatusPending {
		return protocol.FromSnapshot(snap, "request is not pending"), nil
	}
	if err := validateApprover(input.Approver, snap.Request.Requester); err != nil {
		return protocol.Response{RequestID: input.RequestID, Status: string(core.StatusRejected), Message: err.Error()}, nil
	}
	details, err := events.MarshalDetails(events.DenialDetails{Approver: input.Approver, Reason: input.Reason})
	if err != nil {
		return protocol.Response{}, err
	}
	event, err := s.log.Append(events.Event{RequestID: input.RequestID, Actor: input.Approver, Type: events.TypeRequestDenied, Details: details})
	if err != nil {
		return protocol.Response{}, err
	}
	if err := s.projection.Apply(event); err != nil {
		return protocol.Response{}, err
	}
	snap, _ = s.projection.Get(input.RequestID)
	return protocol.FromSnapshot(snap, "request denied"), nil
}

func (s *Service) Execute(ctx context.Context, requestID string) (protocol.Response, error) {
	s.mu.Lock()
	if err := s.expireIfNeededLocked(requestID); err != nil {
		s.mu.Unlock()
		return protocol.Response{}, err
	}
	snap, ok := s.projection.Get(requestID)
	if !ok {
		s.mu.Unlock()
		return protocol.Response{RequestID: requestID, Status: string(core.StatusError), Message: "request not found"}, nil
	}
	switch snap.Status {
	case core.StatusExecuting:
		resp := protocol.FromSnapshot(snap, "request is already executing")
		s.mu.Unlock()
		return resp, nil
	case core.StatusExecuted, core.StatusFailed:
		resp := protocol.FromSnapshot(snap, "request already reached a terminal execution state")
		s.mu.Unlock()
		return resp, nil
	case core.StatusDenied, core.StatusExpired, core.StatusRevoked:
		resp := protocol.FromSnapshot(snap, "request is not approved")
		s.mu.Unlock()
		return resp, nil
	case core.StatusPending:
		resp := protocol.FromSnapshot(snap, "request is not approved")
		s.mu.Unlock()
		resp.Status = string(core.StatusRejected)
		return resp, nil
	case core.StatusApproved:
		// continue
	default:
		resp := protocol.FromSnapshot(snap, "request is not approved")
		s.mu.Unlock()
		return resp, nil
	}

	startedDetails, err := events.MarshalDetails(events.ExecutionStartedDetails{Executor: s.executor.Name(), Digest: snap.Request.Digest})
	if err != nil {
		s.mu.Unlock()
		return protocol.Response{}, err
	}
	startedEvent, err := s.log.Append(events.Event{RequestID: requestID, Actor: core.Actor{Kind: "broker", ID: "request-sudod"}, Type: events.TypeExecutionStarted, Details: startedDetails})
	if err != nil {
		s.mu.Unlock()
		return protocol.Response{}, err
	}
	if err := s.projection.Apply(startedEvent); err != nil {
		s.mu.Unlock()
		return protocol.Response{}, err
	}
	request := snap.Request
	s.mu.Unlock()

	result, execErr := s.executor.Execute(ctx, request)
	resultDetails, err := events.MarshalDetails(events.ExecutionFinishedDetails{Result: result})
	if err != nil {
		return protocol.Response{}, err
	}
	finalType := events.TypeExecutionSucceeded
	message := "request executed"
	if execErr != nil {
		finalType = events.TypeExecutionFailed
		message = execErr.Error()
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	finalEvent, err := s.log.Append(events.Event{RequestID: requestID, Actor: core.Actor{Kind: "broker", ID: "request-sudod"}, Type: finalType, Details: resultDetails})
	if err != nil {
		return protocol.Response{}, err
	}
	if err := s.projection.Apply(finalEvent); err != nil {
		return protocol.Response{}, err
	}
	snap, _ = s.projection.Get(requestID)
	return protocol.FromSnapshot(snap, message), nil
}

func (s *Service) expireIfNeededLocked(requestID string) error {
	snap, ok := s.projection.Get(requestID)
	if !ok || snap.Status != core.StatusPending {
		return nil
	}
	if s.now().Before(snap.Request.ExpiresAt) {
		return nil
	}
	event, err := s.log.Append(events.Event{RequestID: requestID, Actor: core.Actor{Kind: "broker", ID: "request-sudod"}, Type: events.TypeRequestExpired})
	if err != nil {
		return err
	}
	return s.projection.Apply(event)
}

func (s *Service) recoverExecutingLocked() error {
	for _, snap := range s.projection.All() {
		if snap.Status != core.StatusExecuting {
			continue
		}
		details, err := events.MarshalDetails(events.RecoveryMarkedFailedDetails{Message: "broker restarted while execution was in flight"})
		if err != nil {
			return err
		}
		event, err := s.log.Append(events.Event{RequestID: snap.Request.ID, Actor: core.Actor{Kind: "broker", ID: "request-sudod"}, Type: events.TypeRecoveryMarkedFailed, Details: details})
		if err != nil {
			return err
		}
		if err := s.projection.Apply(event); err != nil {
			return err
		}
	}
	return nil
}

func digestRequest(requestID string, spec core.ExecutionSpec, requester core.PeerIdentity, expiry time.Time) string {
	envKeys := make([]string, 0, len(spec.Env))
	for key := range spec.Env {
		envKeys = append(envKeys, key)
	}
	sort.Strings(envKeys)
	env := make([]string, 0, len(envKeys))
	for _, key := range envKeys {
		env = append(env, key+"="+spec.Env[key])
	}
	payload, _ := json.Marshal(struct {
		Argv      []string `json:"argv"`
		Env       []string `json:"env"`
		Cwd       string   `json:"cwd"`
		Requester uint32   `json:"requester_uid"`
		RequestID string   `json:"request_id"`
		Expiry    string   `json:"expiry"`
	}{
		Argv:      spec.Argv,
		Env:       env,
		Cwd:       spec.Cwd,
		Requester: requester.UID,
		RequestID: requestID,
		Expiry:    expiry.UTC().Format(time.RFC3339Nano),
	})
	digest := sha256.Sum256(payload)
	return hex.EncodeToString(digest[:])
}

func actorIDForPeer(peer core.PeerIdentity) string {
	if peer.Username != "" {
		return peer.Username
	}
	return fmt.Sprintf("uid:%d", peer.UID)
}

func validateApprover(approver core.Actor, requester core.PeerIdentity) error {
	if approver.Kind == "" || approver.ID == "" {
		return errors.New("approver identity is required")
	}
	requesterUID := strconv.Itoa(int(requester.UID))
	if approver.ID == requester.Username || approver.ID == requesterUID || approver.ID == "uid:"+requesterUID {
		return errors.New("self-approval is forbidden")
	}
	return nil
}

func randomID(prefix string) string {
	buf := make([]byte, 6)
	if _, err := rand.Read(buf); err != nil {
		return fmt.Sprintf("%s_%d", prefix, time.Now().UnixNano())
	}
	return prefix + "_" + hex.EncodeToString(buf)
}
