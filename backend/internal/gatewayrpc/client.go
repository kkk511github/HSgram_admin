package gatewayrpc

import (
	"context"
	"fmt"
	"strings"
	"time"

	gatewaypb "github.com/teamgram/teamgram-server/app/interface/gnetway/gateway"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type Client struct {
	addr string
	conn *grpc.ClientConn
	rpc  gatewaypb.RPCGatewayClient
}

func New(addr string) (*Client, error) {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return nil, fmt.Errorf("gateway rpc address is required")
	}

	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, fmt.Errorf("connect to gateway rpc: %w", err)
	}

	return &Client{addr: addr, conn: conn, rpc: gatewaypb.NewRPCGatewayClient(conn)}, nil
}

func (c *Client) Close() error {
	if c == nil || c.conn == nil {
		return nil
	}
	return c.conn.Close()
}

func (c *Client) ForceDisconnectAuthKeys(ctx context.Context, authKeyIDs []int64) error {
	if c == nil || c.rpc == nil {
		return fmt.Errorf("gateway rpc client is not configured")
	}

	for _, authKeyID := range authKeyIDs {
		if authKeyID == 0 {
			continue
		}
		callCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		_, err := c.rpc.GatewaySendDataToGateway(callCtx, &gatewaypb.TLGatewaySendDataToGateway{
			AuthKeyId: authKeyID,
			SessionId: 0,
			Payload:   nil,
		})
		cancel()
		if err != nil {
			return fmt.Errorf("disconnect auth_key_id %d via %s: %w", authKeyID, c.addr, err)
		}
	}

	return nil
}
