package svc

import (
	"awesomeProject/chat-rpc/internal/config"
	"awesomeProject/chat-rpc/internal/lib"
)

type ServiceContext struct {
	Config     config.Config
	AgentReg   *lib.Registry
	SessionCli interface{} // 后续添加 session-rpc 客户端
	RAGCli     interface{} // 后续添加 rag-rpc 客户端
}

func NewServiceContext(c config.Config, agentReg *lib.Registry) *ServiceContext {
	return &ServiceContext{
		Config:   c,
		AgentReg: agentReg,
	}
}
