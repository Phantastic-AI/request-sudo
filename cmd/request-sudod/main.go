package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"

	"request-sudo/internal/broker"
	"request-sudo/internal/events"
	"request-sudo/internal/execution"
	"request-sudo/internal/socket"
)

func main() {
	var (
		requestSocket = flag.String("request-socket", "/run/request-sudo/request.sock", "path to requester Unix socket")
		reviewSocket  = flag.String("review-socket", "/run/request-sudo/review.sock", "path to review Unix socket")
		eventLog      = flag.String("event-log", "/var/lib/request-sudo/events.jsonl", "path to append-only event log")
		reviewUIDs    = flag.String("review-uids", strconv.Itoa(os.Geteuid()), "comma-separated list of UIDs allowed on the review socket")
	)
	flag.Parse()

	log, err := events.NewLog(*eventLog)
	if err != nil {
		fail(err)
	}
	service, err := broker.NewService(log, execution.LocalExecutor{})
	if err != nil {
		fail(err)
	}
	server := socket.NewServer(service, *requestSocket, *reviewSocket, parseUIDs(*reviewUIDs))

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	fmt.Fprintf(os.Stderr, "request-sudod listening on %s and %s\n", *requestSocket, *reviewSocket)
	if err := server.Run(ctx); err != nil && ctx.Err() == nil {
		fail(err)
	}
}

func parseUIDs(raw string) []uint32 {
	parts := strings.Split(raw, ",")
	out := make([]uint32, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		value, err := strconv.ParseUint(part, 10, 32)
		if err != nil {
			continue
		}
		out = append(out, uint32(value))
	}
	if len(out) == 0 {
		out = append(out, uint32(os.Geteuid()))
	}
	return out
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
