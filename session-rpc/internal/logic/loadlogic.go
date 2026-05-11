package logic

import (
	"context"

	"awesomeProject/session-rpc/awesomeProject/session-rpc/session"
	"awesomeProject/session-rpc/internal/svc"

	"github.com/zeromicro/go-zero/core/logx"
)

type LoadLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewLoadLogic(ctx context.Context, svcCtx *svc.ServiceContext) *LoadLogic {
	return &LoadLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// 加载会话历史
func (l *LoadLogic) Load(in *session.LoadReq) (*session.LoadResp, error) {
	if l.svcCtx.DB == nil {
		return &session.LoadResp{SessionId: in.SessionId}, nil
	}

	rows, err := l.svcCtx.DB.Query(l.ctx, `
		SELECT id, role, content, created_at FROM chat_messages
		WHERE session_id = $1
		ORDER BY created_at ASC
	`, in.SessionId)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var messages []*session.Message
	for rows.Next() {
		var msg session.Message
		if err := rows.Scan(&msg.Id, &msg.Role, &msg.Content, &msg.CreatedAt); err != nil {
			continue
		}
		messages = append(messages, &msg)
	}

	return &session.LoadResp{
		SessionId: in.SessionId,
		Messages:  messages,
	}, nil
}
