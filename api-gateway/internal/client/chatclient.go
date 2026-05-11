package client

import (
	"context"
	"fmt"
	"time"

	chat "awesomeProject/api-gateway/internal/proto"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// ChatClient chat-rpc 客户端
type ChatClient struct {
	conn   *grpc.ClientConn
	client chat.ChatServiceClient
}

// NewChatClient 创建 chat-rpc 客户端
func NewChatClient(endpoints []string) (*ChatClient, error) {
	if len(endpoints) == 0 {
		endpoints = []string{"127.0.0.1:8083"}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, err := grpc.DialContext(ctx, endpoints[0],
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithBlock(),
	)
	if err != nil {
		return nil, fmt.Errorf("连接 chat-rpc 失败: %v", err)
	}

	return &ChatClient{
		conn:   conn,
		client: chat.NewChatServiceClient(conn),
	}, nil
}

// Stream 流式聊天
func (c *ChatClient) Stream(ctx context.Context) (chat.ChatService_StreamClient, error) {
	return c.client.Stream(ctx)
}

// Chat 非流式聊天
func (c *ChatClient) Chat(ctx context.Context, req *chat.ChatReq) (*chat.ChatResp, error) {
	return c.client.Chat(ctx, req)
}

// Close 关闭连接
func (c *ChatClient) Close() error {
	if c.conn != nil {
		return c.conn.Close()
	}
	return nil
}
