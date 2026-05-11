package logic

import (
	"context"
	"fmt"

	"awesomeProject/session-rpc/awesomeProject/session-rpc/session"
	"awesomeProject/session-rpc/internal/svc"

	"github.com/zeromicro/go-zero/core/logx"
)

type SaveLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewSaveLogic(ctx context.Context, svcCtx *svc.ServiceContext) *SaveLogic {
	return &SaveLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// 保存单条消息
func (l *SaveLogic) Save(in *session.SaveReq) (*session.SaveResp, error) {
	if l.svcCtx.DB == nil {
		return &session.SaveResp{SessionId: in.SessionId}, nil
	}

	// upsert session
	_, err := l.svcCtx.DB.Exec(l.ctx, `
		INSERT INTO chat_sessions (session_id, updated_at) VALUES ($1, NOW())
		ON CONFLICT (session_id) DO UPDATE SET updated_at = NOW()
	`, in.SessionId)
	if err != nil {
		return nil, err
	}

	// insert message
	var msgID int
	err = l.svcCtx.DB.QueryRow(l.ctx, `
		INSERT INTO chat_messages (session_id, role, content) VALUES ($1, $2, $3)
		RETURNING id
	`, in.SessionId, in.Role, in.Content).Scan(&msgID)
	if err != nil {
		return nil, err
	}

	return &session.SaveResp{
		Id:        fmt.Sprintf("%d", msgID),
		SessionId: in.SessionId,
	}, nil
}
