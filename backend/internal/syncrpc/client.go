package syncrpc

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/teamgram/proto/mtproto"
	syncpb "github.com/teamgram/teamgram-server/app/messenger/sync/sync"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type Client struct {
	addr string
	conn *grpc.ClientConn
	rpc  syncpb.RPCSyncClient
}

func New(addr string) (*Client, error) {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return nil, fmt.Errorf("sync rpc address is required")
	}

	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, fmt.Errorf("connect to sync rpc: %w", err)
	}

	return &Client{addr: addr, conn: conn, rpc: syncpb.NewRPCSyncClient(conn)}, nil
}

func (c *Client) Close() error {
	if c == nil || c.conn == nil {
		return nil
	}
	return c.conn.Close()
}

func (c *Client) PushUserPopup(ctx context.Context, userID int64, message string) error {
	if c == nil || c.rpc == nil || strings.TrimSpace(message) == "" {
		return nil
	}

	callCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	update := mtproto.MakeTLUpdateServiceNotification(&mtproto.Update{
		Popup:          true,
		InboxDate:      mtproto.MakeFlagsInt32(int32(time.Now().Unix())),
		Type:           fmt.Sprintf("admin_notice_%d", time.Now().UnixNano()),
		Message_STRING: message,
		Media:          mtproto.MakeTLMessageMediaEmpty(nil).To_MessageMedia(),
	}).To_Update()

	_, err := c.rpc.SyncPushUpdates(callCtx, &syncpb.TLSyncPushUpdates{
		UserId:  userID,
		Updates: mtproto.MakeUpdatesByUpdates(update),
	})
	if err != nil {
		return fmt.Errorf("push popup via %s: %w", c.addr, err)
	}
	return nil
}

func (c *Client) PushResetAuthorization(ctx context.Context, userID int64, authKeyIDs []int64) error {
	if c == nil || c.rpc == nil || len(authKeyIDs) == 0 {
		return nil
	}

	for _, keyID := range authKeyIDs {
		callCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		_, err := c.rpc.SyncUpdatesMe(callCtx, &syncpb.TLSyncUpdatesMe{
			UserId:        userID,
			PermAuthKeyId: keyID,
			Updates:       mtproto.MakeTLUpdatesTooLong(nil).To_Updates(),
		})
		cancel()
		if err != nil {
			return fmt.Errorf("push updatesTooLong via %s: %w", c.addr, err)
		}

		callCtx, cancel = context.WithTimeout(ctx, 10*time.Second)
		_, err = c.rpc.SyncUpdatesMe(callCtx, &syncpb.TLSyncUpdatesMe{
			UserId:        userID,
			PermAuthKeyId: keyID,
			Updates: mtproto.MakeTLUpdateAccountResetAuthorization(&mtproto.Updates{
				UserId:    userID,
				AuthKeyId: keyID,
			}).To_Updates(),
		})
		cancel()
		if err != nil {
			return fmt.Errorf("push reset authorization via %s: %w", c.addr, err)
		}
	}

	return nil
}
