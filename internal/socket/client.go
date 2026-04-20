package socket

import (
	"context"
	"encoding/json"
	"net"

	"lease-broker-successor/internal/protocol"
)

func Call(ctx context.Context, socketPath string, request protocol.Request) (protocol.Response, error) {
	var response protocol.Response
	dialer := net.Dialer{}
	conn, err := dialer.DialContext(ctx, "unix", socketPath)
	if err != nil {
		return response, err
	}
	defer conn.Close()

	encoder := json.NewEncoder(conn)
	decoder := json.NewDecoder(conn)
	if err := encoder.Encode(request); err != nil {
		return response, err
	}
	if err := decoder.Decode(&response); err != nil {
		return response, err
	}
	return response, nil
}
