package logic

import (
	"context"
	"io"

	"github.com/cloudwego/eino/schema"

	"awesomeProject/chat-rpc/awesomeProject/chat-rpc/chat"
	"awesomeProject/chat-rpc/internal/svc"

	"github.com/zeromicro/go-zero/core/logx"
)

type ChatLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewChatLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ChatLogic {
	return &ChatLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// 非流式聊天
func (l *ChatLogic) Chat(in *chat.ChatReq) (*chat.ChatResp, error) {
	modelID := in.Model
	if modelID == "" {
		modelID = "kimi-k2"
	}

	agent, ok := l.svcCtx.AgentReg.Get(modelID)
	if !ok {
		return &chat.ChatResp{
			SessionId: in.SessionId,
			Model:     modelID,
		}, nil
	}

	// 构建消息历史
	var messages []*schema.Message
	if len(in.History) > 0 {
		for _, m := range in.History {
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
	messages = append(messages, schema.UserMessage(in.Message))

	// 调用 Agent Stream 并收集所有内容
	result, err := agent.Stream(l.ctx, messages)
	if err != nil {
		return &chat.ChatResp{
			SessionId: in.SessionId,
			Model:     modelID,
			Content:   "",
		}, err
	}

	var content string
	for {
		msg, err := result.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			break
		}
		if msg != nil {
			content += msg.Content
		}
	}

	// 匹配图片
	imageURL := l.svcCtx.AgentReg.MatchImage(in.Message, content)

	return &chat.ChatResp{
		SessionId:  in.SessionId,
		Model:      modelID,
		Content:    content,
		Image:      imageURL,
		TokenCount: int32(len([]rune(content))),
	}, nil
}
