package config

import "github.com/zeromicro/go-zero/zrpc"

type DatabaseConf struct {
	DSN string
}

type Config struct {
	zrpc.RpcServerConf
	Database DatabaseConf
}
