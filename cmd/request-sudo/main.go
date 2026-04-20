package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"request-sudo/internal/core"
	"request-sudo/internal/protocol"
	"request-sudo/internal/socket"
)

func main() {
	if len(os.Args) < 2 {
		usage()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	switch os.Args[1] {
	case "request":
		handleRequest(ctx, os.Args[2:])
	case "status":
		handleStatus(ctx, os.Args[2:])
	case "execute":
		handleExecute(ctx, os.Args[2:])
	default:
		usage()
	}
}

func handleRequest(ctx context.Context, args []string) {
	fs := flag.NewFlagSet("request", flag.ExitOnError)
	socketPath := fs.String("socket", "/run/request-sudo/request.sock", "request socket path")
	reason := fs.String("reason", "", "human-readable reason")
	mode := fs.String("mode", string(core.ModePoll), "request mode: poll or wait")
	cwd := fs.String("cwd", mustGetwd(), "execution cwd to freeze into the digest")
	fs.Parse(args)
	argv := fs.Args()
	if len(argv) == 0 {
		failf("request requires argv after flags")
	}
	resp, err := socket.Call(ctx, *socketPath, protocol.Request{Action: protocol.ActionRequestSubmit, Argv: argv, Reason: *reason, Mode: core.Mode(*mode), Cwd: *cwd})
	if err != nil {
		fail(err)
	}
	printJSON(resp)
}

func handleStatus(ctx context.Context, args []string) {
	fs := flag.NewFlagSet("status", flag.ExitOnError)
	socketPath := fs.String("socket", "/run/request-sudo/request.sock", "request socket path")
	fs.Parse(args)
	if fs.NArg() != 1 {
		failf("status requires exactly one request id")
	}
	resp, err := socket.Call(ctx, *socketPath, protocol.Request{Action: protocol.ActionRequestStatus, RequestID: fs.Arg(0)})
	if err != nil {
		fail(err)
	}
	printJSON(resp)
}

func handleExecute(ctx context.Context, args []string) {
	fs := flag.NewFlagSet("execute", flag.ExitOnError)
	socketPath := fs.String("socket", "/run/request-sudo/request.sock", "request socket path")
	fs.Parse(args)
	if fs.NArg() != 1 {
		failf("execute requires exactly one request id")
	}
	resp, err := socket.Call(ctx, *socketPath, protocol.Request{Action: protocol.ActionRequestExecute, RequestID: fs.Arg(0)})
	if err != nil {
		fail(err)
	}
	printJSON(resp)
}

func printJSON(v any) {
	payload, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		fail(err)
	}
	fmt.Println(string(payload))
}

func mustGetwd() string {
	cwd, err := os.Getwd()
	if err != nil {
		return "/"
	}
	return cwd
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: request-sudo <request|status|execute> [args]")
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
