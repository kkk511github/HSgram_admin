package authsessionrpc

import (
	"context"
	"fmt"
	"strings"
	"time"

	authpb "github.com/teamgram/teamgram-server/app/service/authsession/authsession"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type Client struct {
	addr string
	conn *grpc.ClientConn
	rpc  authpb.RPCAuthsessionClient
}

func New(addr string) (*Client, error) {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return nil, fmt.Errorf("authsession rpc address is required")
	}

	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, fmt.Errorf("connect to authsession rpc: %w", err)
	}

	return &Client{addr: addr, conn: conn, rpc: authpb.NewRPCAuthsessionClient(conn)}, nil
}

func (c *Client) Close() error {
	if c == nil || c.conn == nil {
		return nil
	}
	return c.conn.Close()
}

func (c *Client) ResetAllAuthorizations(ctx context.Context, userID int64) ([]int64, error) {
	if c == nil || c.rpc == nil {
		return nil, fmt.Errorf("authsession rpc client is not configured")
	}

	callCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	resp, err := c.rpc.AuthsessionResetAuthorization(callCtx, &authpb.TLAuthsessionResetAuthorization{UserId: userID, AuthKeyId: 0, Hash: 0})
	if err != nil {
		return nil, fmt.Errorf("reset authorizations via %s: %w", c.addr, err)
	}

	return resp.GetDatas(), nil
}
