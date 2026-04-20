package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/user"
	"time"

	"lease-broker-successor/internal/core"
	"lease-broker-successor/internal/protocol"
	"lease-broker-successor/internal/socket"
)

func main() {
	if len(os.Args) < 2 {
		usage()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	switch os.Args[1] {
	case "approve":
		handleApprove(ctx, os.Args[2:])
	case "deny":
		handleDeny(ctx, os.Args[2:])
	default:
		usage()
	}
}

func handleApprove(ctx context.Context, args []string) {
	fs := flag.NewFlagSet("approve", flag.ExitOnError)
	socketPath := fs.String("socket", "/run/lb/review.sock", "review socket path")
	approverKind := fs.String("approver-kind", "local", "approver identity kind")
	approverID := fs.String("approver-id", currentUser(), "approver identity id")
	totp := fs.String("totp", "", "optional TOTP code")
	fs.Parse(args)
	if fs.NArg() != 1 {
		failf("approve requires exactly one request id")
	}
	resp, err := socket.Call(ctx, *socketPath, protocol.Request{Action: protocol.ActionReviewApprove, RequestID: fs.Arg(0), Approver: core.Actor{Kind: *approverKind, ID: *approverID}, TOTP: *totp})
	if err != nil {
		fail(err)
	}
	printJSON(resp)
}

func handleDeny(ctx context.Context, args []string) {
	fs := flag.NewFlagSet("deny", flag.ExitOnError)
	socketPath := fs.String("socket", "/run/lb/review.sock", "review socket path")
	approverKind := fs.String("approver-kind", "local", "approver identity kind")
	approverID := fs.String("approver-id", currentUser(), "approver identity id")
	reason := fs.String("reason", "Denied by approver", "denial reason")
	fs.Parse(args)
	if fs.NArg() != 1 {
		failf("deny requires exactly one request id")
	}
	resp, err := socket.Call(ctx, *socketPath, protocol.Request{Action: protocol.ActionReviewDeny, RequestID: fs.Arg(0), Approver: core.Actor{Kind: *approverKind, ID: *approverID}, Reason: *reason})
	if err != nil {
		fail(err)
	}
	printJSON(resp)
}

func currentUser() string {
	usr, err := user.Current()
	if err != nil {
		return fmt.Sprintf("uid:%d", os.Geteuid())
	}
	return usr.Username
}

func printJSON(v any) {
	payload, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		fail(err)
	}
	fmt.Println(string(payload))
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: lbctl <approve|deny> [args]")
	os.Exit(2)
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}

func failf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
