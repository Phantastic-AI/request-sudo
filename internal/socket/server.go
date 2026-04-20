package socket

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"sync"
	"syscall"

	"request-sudo/internal/broker"
	"request-sudo/internal/core"
	"request-sudo/internal/protocol"
)

type Server struct {
	service           *broker.Service
	requestSocketPath string
	reviewSocketPath  string
	reviewUIDs        map[uint32]struct{}
	reviewGIDs        map[uint32]struct{}
}

func NewServer(service *broker.Service, requestSocketPath, reviewSocketPath string, reviewUIDs []uint32, reviewGIDs []uint32) *Server {
	allowUIDs := make(map[uint32]struct{}, len(reviewUIDs))
	for _, uid := range reviewUIDs {
		allowUIDs[uid] = struct{}{}
	}
	allowGIDs := make(map[uint32]struct{}, len(reviewGIDs))
	for _, gid := range reviewGIDs {
		allowGIDs[gid] = struct{}{}
	}
	return &Server{
		service:           service,
		requestSocketPath: requestSocketPath,
		reviewSocketPath:  reviewSocketPath,
		reviewUIDs:        allowUIDs,
		reviewGIDs:        allowGIDs,
	}
}

func (s *Server) Run(ctx context.Context) error {
	requestListener, err := listenSocket(s.requestSocketPath, 0o660)
	if err != nil {
		return err
	}
	defer requestListener.Close()
	reviewListener, err := listenSocket(s.reviewSocketPath, 0o600)
	if err != nil {
		return err
	}
	defer reviewListener.Close()

	var wg sync.WaitGroup
	serve := func(listener *net.UnixListener, lane string) {
		defer wg.Done()
		for {
			conn, err := listener.AcceptUnix()
			if err != nil {
				select {
				case <-ctx.Done():
					return
				default:
					continue
				}
			}
			go s.handleConn(ctx, conn, lane)
		}
	}

	wg.Add(2)
	go serve(requestListener, "request")
	go serve(reviewListener, "review")
	<-ctx.Done()
	_ = requestListener.Close()
	_ = reviewListener.Close()
	wg.Wait()
	return nil
}

func (s *Server) handleConn(ctx context.Context, conn *net.UnixConn, lane string) {
	defer conn.Close()
	peer, err := peerIdentity(conn)
	if err != nil {
		peer = core.PeerIdentity{Username: "unknown"}
	}

	var request protocol.Request
	decoder := json.NewDecoder(conn)
	encoder := json.NewEncoder(conn)
	if err := decoder.Decode(&request); err != nil {
		_ = encoder.Encode(protocol.Response{Status: string(core.StatusError), Message: err.Error()})
		return
	}

	response, err := s.dispatch(ctx, lane, request, peer)
	if err != nil {
		response = protocol.Response{RequestID: request.RequestID, Status: string(core.StatusError), Message: err.Error()}
	}
	_ = encoder.Encode(response)
}

func (s *Server) dispatch(ctx context.Context, lane string, request protocol.Request, peer core.PeerIdentity) (protocol.Response, error) {
	switch request.Action {
	case protocol.ActionRequestSubmit:
		if lane != "request" {
			return protocol.Response{Status: string(core.StatusRejected), Message: "submit is only allowed on request socket"}, nil
		}
		return s.service.Submit(ctx, broker.SubmitInput{Argv: request.Argv, Reason: request.Reason, Mode: request.Mode, Requester: peer, Cwd: request.Cwd})
	case protocol.ActionRequestStatus:
		if lane != "request" {
			return protocol.Response{Status: string(core.StatusRejected), Message: "status is only allowed on request socket"}, nil
		}
		return s.service.Status(ctx, request.RequestID)
	case protocol.ActionRequestExecute:
		if lane != "request" {
			return protocol.Response{Status: string(core.StatusRejected), Message: "execute is only allowed on request socket"}, nil
		}
		return s.service.Execute(ctx, request.RequestID)
	case protocol.ActionReviewApprove:
		if lane != "review" {
			return protocol.Response{Status: string(core.StatusRejected), Message: "approve is only allowed on review socket"}, nil
		}
		if !s.reviewAllowed(peer.UID, peer.GID) {
			return protocol.Response{RequestID: request.RequestID, Status: string(core.StatusRejected), Message: "peer uid is not allowed on review socket"}, nil
		}
		return s.service.Approve(ctx, broker.ReviewInput{RequestID: request.RequestID, Approver: request.Approver, ApprovalCode: request.ApprovalCode})
	case protocol.ActionReviewDeny:
		if lane != "review" {
			return protocol.Response{Status: string(core.StatusRejected), Message: "deny is only allowed on review socket"}, nil
		}
		if !s.reviewAllowed(peer.UID, peer.GID) {
			return protocol.Response{RequestID: request.RequestID, Status: string(core.StatusRejected), Message: "peer uid is not allowed on review socket"}, nil
		}
		return s.service.Deny(ctx, broker.ReviewInput{RequestID: request.RequestID, Approver: request.Approver, Reason: request.Reason})
	default:
		return protocol.Response{RequestID: request.RequestID, Status: string(core.StatusError), Message: fmt.Sprintf("unsupported action %s", request.Action)}, nil
	}
}

func (s *Server) reviewAllowed(uid, gid uint32) bool {
	if _, ok := s.reviewUIDs[uid]; ok {
		return true
	}
	if _, ok := s.reviewGIDs[gid]; ok {
		return true
	}
	return false
}

func listenSocket(path string, mode os.FileMode) (*net.UnixListener, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	_ = os.Remove(path)
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		return nil, err
	}
	if err := os.Chmod(path, mode); err != nil {
		listener.Close()
		return nil, err
	}
	return listener, nil
}

func peerIdentity(conn *net.UnixConn) (core.PeerIdentity, error) {
	var identity core.PeerIdentity
	rawConn, err := conn.SyscallConn()
	if err != nil {
		return identity, err
	}
	var innerErr error
	if err := rawConn.Control(func(fd uintptr) {
		cred, err := syscall.GetsockoptUcred(int(fd), syscall.SOL_SOCKET, syscall.SO_PEERCRED)
		if err != nil {
			innerErr = err
			return
		}
		identity.UID = cred.Uid
		identity.GID = cred.Gid
		identity.PID = cred.Pid
	}); err != nil {
		return identity, err
	}
	if innerErr != nil {
		return identity, innerErr
	}
	usr, err := user.LookupId(strconv.Itoa(int(identity.UID)))
	if err == nil {
		identity.Username = usr.Username
	}
	return identity, nil
}
