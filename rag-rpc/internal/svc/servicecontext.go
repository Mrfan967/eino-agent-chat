package svc

import (
	"awesomeProject/rag-rpc/internal/config"
	"awesomeProject/rag-rpc/internal/lib"
)

// RAGService 封装 RAG 检索服务
type RAGService struct {
	Store    *lib.DocumentStore
	Embedder interface{} // einoembedding.Embedder 类型，使用 interface{} 避免循环依赖
	APIKey   string
	BaseURL  string
	Model    string
}

type ServiceContext struct {
	Config config.Config
	RAG    *RAGService
}

func NewServiceContext(c config.Config, rag *RAGService) *ServiceContext {
	return &ServiceContext{
		Config: c,
		RAG:    rag,
	}
}
