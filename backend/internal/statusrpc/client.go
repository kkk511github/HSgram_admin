package statusrpc

import (
	"context"
	"fmt"
	"strings"
	"time"

	statuspb "github.com/teamgram/teamgram-server/app/service/status/status"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type Client struct {
	addr string
	conn *grpc.ClientConn
	rpc  statuspb.RPCStatusClient
}

func New(addr string) (*Client, error) {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return nil, fmt.Errorf("status rpc address is required")
	}

	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, fmt.Errorf("connect to status rpc: %w", err)
	}

	return &Client{addr: addr, conn: conn, rpc: statuspb.NewRPCStatusClient(conn)}, nil
}

func (c *Client) Close() error {
	if c == nil || c.conn == nil {
		return nil
	}
	return c.conn.Close()
}

func (c *Client) GetOnlineAuthKeys(ctx context.Context, userID int64) ([]int64, error) {
	if c == nil || c.rpc == nil {
		return nil, fmt.Errorf("status rpc client is not configured")
	}

	callCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	resp, err := c.rpc.StatusGetUserOnlineSessions(callCtx, &statuspb.TLStatusGetUserOnlineSessions{UserId: userID})
	if err != nil {
		return nil, fmt.Errorf("get online sessions via %s: %w", c.addr, err)
	}

	ids := make([]int64, 0, len(resp.GetUserSessions()))
	for _, sess := range resp.GetUserSessions() {
		if sess.GetAuthKeyId() != 0 {
			ids = append(ids, sess.GetAuthKeyId())
		}
	}
	return ids, nil
}
