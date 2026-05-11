package config

import "github.com/zeromicro/go-zero/zrpc"

type EmbeddingConf struct {
	APIKey  string
	BaseURL string
	Model   string
}

type KnowledgeConf struct {
	Path string
}

type DatabaseConf struct {
	DSN string
}

type Config struct {
	zrpc.RpcServerConf
	Embedding EmbeddingConf
	Knowledge KnowledgeConf
	Database  DatabaseConf
}
