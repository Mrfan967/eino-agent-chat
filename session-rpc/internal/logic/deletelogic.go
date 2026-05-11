package logic

import (
	"context"

	"awesomeProject/session-rpc/awesomeProject/session-rpc/session"
	"awesomeProject/session-rpc/internal/svc"

	"github.com/zeromicro/go-zero/core/logx"
)

type DeleteLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewDeleteLogic(ctx context.Context, svcCtx *svc.ServiceContext) *DeleteLogic {
	return &DeleteLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// 删除整个会话
func (l *DeleteLogic) Delete(in *session.DeleteReq) (*session.DeleteResp, error) {
	if l.svcCtx.DB == nil {
		return &session.DeleteResp{Success: true}, nil
	}

	_, err := l.svcCtx.DB.Exec(l.ctx, `DELETE FROM chat_sessions WHERE session_id = $1`, in.SessionId)
	if err != nil {
		return &session.DeleteResp{Success: false}, err
	}

	return &session.DeleteResp{Success: true}, nil
}
