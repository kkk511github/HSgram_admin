package messenger

import (
	"context"
	"fmt"
	"math/rand"
	"strings"
	"time"

	"github.com/teamgram/proto/mtproto"
	msgpb "github.com/teamgram/teamgram-server/app/messenger/msg/msg/msg"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type Client struct {
	addr string
	conn *grpc.ClientConn
	rpc  msgpb.RPCMsgClient
}

func New(addr string) (*Client, error) {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return nil, fmt.Errorf("message rpc address is required")
	}

	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, fmt.Errorf("connect to msg rpc: %w", err)
	}

	return &Client{
		addr: addr,
		conn: conn,
		rpc:  msgpb.NewRPCMsgClient(conn),
	}, nil
}

func (c *Client) Close() error {
	if c == nil || c.conn == nil {
		return nil
	}
	return c.conn.Close()
}

func (c *Client) SendTextMessage(ctx context.Context, senderUserID, targetUserID int64, text string) error {
	if c == nil || c.rpc == nil {
		return fmt.Errorf("message rpc client is not configured")
	}

	messageText := strings.TrimSpace(text)
	if messageText == "" {
		return fmt.Errorf("message text is empty")
	}

	message := mtproto.MakeTLMessage(&mtproto.Message{
		Out:     true,
		Date:    int32(time.Now().Unix()),
		FromId:  mtproto.MakePeerUser(senderUserID),
		PeerId:  mtproto.MakeTLPeerUser(&mtproto.Peer{UserId: targetUserID}).To_Peer(),
		Message: messageText,
	}).To_Message()

	req := &msgpb.TLMsgPushUserMessage{
		Constructor: msgpb.TLConstructor_CRC32_msg_pushUserMessage,
		UserId:      senderUserID,
		AuthKeyId:   0,
		PeerType:    mtproto.PEER_USER,
		PeerId:      targetUserID,
		PushType:    1,
		Message: msgpb.MakeTLOutboxMessage(&msgpb.OutboxMessage{
			Constructor:  msgpb.TLConstructor_CRC32_outboxMessage,
			NoWebpage:    false,
			Background:   false,
			RandomId:     rand.Int63(),
			Message:      message,
			ScheduleDate: nil,
		}).To_OutboxMessage(),
	}

	callCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	_, err := c.rpc.MsgPushUserMessage(callCtx, req)
	if err != nil {
		return fmt.Errorf("push user message via %s: %w", c.addr, err)
	}

	return nil
}
