package logic

import (
	"context"
	"io"

	"github.com/cloudwego/eino/schema"

	"awesomeProject/chat-rpc/awesomeProject/chat-rpc/chat"
	"awesomeProject/chat-rpc/internal/svc"

	"github.com/zeromicro/go-zero/core/logx"
)

type StreamLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewStreamLogic(ctx context.Context, svcCtx *svc.ServiceContext) *StreamLogic {
	return &StreamLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// 流式聊天 - 双向流处理
func (l *StreamLogic) Stream(stream chat.ChatService_StreamServer) error {
	// 接收第一个请求（包含 session_id, model, message）
	req, err := stream.Recv()
	if err != nil {
		return err
	}

	modelID := req.Model
	if modelID == "" {
		modelID = "kimi-k2"
	}

	agent, ok := l.svcCtx.AgentReg.Get(modelID)
	if !ok {
		stream.Send(&chat.ChatChunk{Error: "不支持的模型"})
		stream.Send(&chat.ChatChunk{Done: true})
		return nil
	}

	// 构建消息历史
	var messages []*schema.Message
	if len(req.History) > 0 {
		for _, m := range req.History {
			var msg *schema.Message
			switch m.Role {
			case chat.Role_USER:
				msg = schema.UserMessage(m.Content)
			case chat.Role_ASSISTANT:
				msg = schema.AssistantMessage(m.Content, nil)
			case chat.Role_SYSTEM:
				msg = schema.SystemMessage(m.Content)
			}
			if msg != nil {
				messages = append(messages, msg)
			}
		}
	}
	messages = append(messages, schema.UserMessage(req.Message))

	// 调用 Agent 流式接口
	result, err := agent.Stream(l.ctx, messages)
	if err != nil {
		stream.Send(&chat.ChatChunk{Error: "Agent 出错: " + err.Error()})
		stream.Send(&chat.ChatChunk{Done: true})
		return nil
	}

	var fullContent string
	for {
		msg, err := result.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			stream.Send(&chat.ChatChunk{Error: "流式读取错误: " + err.Error()})
			break
		}
		if msg == nil || msg.Content == "" {
			continue
		}
		fullContent += msg.Content
		stream.Send(&chat.ChatChunk{Content: msg.Content})
	}

	// 匹配图片
	imageURL := l.svcCtx.AgentReg.MatchImage(req.Message, fullContent)
	if imageURL != "" {
		stream.Send(&chat.ChatChunk{Image: imageURL})
	}

	// 发送结束标记
	stream.Send(&chat.ChatChunk{Done: true})
	return nil
}
