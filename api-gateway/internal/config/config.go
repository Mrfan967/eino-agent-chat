package config

import "github.com/zeromicro/go-zero/rest"

// ChatRpcConf chat-rpc 服务配置
type ChatRpcConf struct {
	Endpoints []string
}

// Config api-gateway 配置
type Config struct {
	rest.RestConf
	ChatRpc ChatRpcConf
}
