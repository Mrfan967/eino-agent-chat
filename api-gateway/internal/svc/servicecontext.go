package svc

import (
	"awesomeProject/api-gateway/internal/config"

	chatrpc "awesomeProject/api-gateway/internal/client"
)

// ServiceContext 服务上下文
type ServiceContext struct {
	Config  config.Config
	ChatCli *chatrpc.ChatClient
}

// NewServiceContext 创建服务上下文
func NewServiceContext(c config.Config, chatCli *chatrpc.ChatClient) *ServiceContext {
	return &ServiceContext{
		Config:  c,
		ChatCli: chatCli,
	}
}
