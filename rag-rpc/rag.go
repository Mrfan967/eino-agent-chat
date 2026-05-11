package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	einoembedding "github.com/cloudwego/eino-ext/components/embedding/openai"

	"awesomeProject/rag-rpc/awesomeProject/rag-rpc/rag"
	"awesomeProject/rag-rpc/internal/config"
	"awesomeProject/rag-rpc/internal/lib"
	"awesomeProject/rag-rpc/internal/server"
	"awesomeProject/rag-rpc/internal/svc"

	"github.com/zeromicro/go-zero/core/conf"
	"github.com/zeromicro/go-zero/core/service"
	"github.com/zeromicro/go-zero/zrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
)

var configFile = flag.String("f", "etc/rag.yaml", "the config file")

// initRAG 初始化 RAG 服务，加载知识库
func initRAG(ctx context.Context, c config.Config) (*svc.RAGService, error) {
	ragSvc := &svc.RAGService{
		Store:   lib.NewDocumentStore(),
		APIKey:  c.Embedding.APIKey,
		BaseURL: c.Embedding.BaseURL,
		Model:   c.Embedding.Model,
	}

	// 初始化 Embedder
	if ragSvc.APIKey == "" {
		fmt.Println("⚠️  API Key 为空，向量检索不可用，将仅使用 BM25 检索。")
	} else {
		emb, err := einoembedding.NewEmbedder(ctx, &einoembedding.EmbeddingConfig{
			APIKey:  ragSvc.APIKey,
			BaseURL: ragSvc.BaseURL,
			Model:   ragSvc.Model,
		})
		if err != nil {
			fmt.Printf("⚠️  初始化 Embedder 失败: %v，将仅使用 BM25 检索。\n", err)
		} else {
			ragSvc.Embedder = emb
		}
	}

	// 读取 knowledge 目录
	knowledgePath := c.Knowledge.Path
	if knowledgePath == "" {
		knowledgePath = "../knowledge"
	}
	files, err := filepath.Glob(filepath.Join(knowledgePath, "*.txt"))
	if err != nil || len(files) == 0 {
		fmt.Println("ℹ️  knowledge 目录下没有找到 .txt 文件，RAG 知识库为空。")
		return ragSvc, nil
	}

	fmt.Printf("📚 开始构建 RAG 知识库，找到 %d 个文档...\n", len(files))

	var allChunks []string
	for _, file := range files {
		content, err := os.ReadFile(file)
		if err != nil {
			fmt.Printf("读取文件 %s 失败: %v\n", file, err)
			continue
		}
		text := string(content)
		if strings.TrimSpace(text) == "" {
			continue
		}
		chunks := lib.ChunkText(text, 400, 50)
		for _, chunk := range chunks {
			allChunks = append(allChunks, fmt.Sprintf("[来源: %s]\n%s", filepath.Base(file), chunk))
		}
	}

	if len(allChunks) == 0 {
		return ragSvc, nil
	}

	fmt.Printf("🔄 正在为 %d 个文本块生成特征向量...\n", len(allChunks))

	const batchSize = 20
	for i := 0; i < len(allChunks); i += batchSize {
		end := i + batchSize
		if end > len(allChunks) {
			end = len(allChunks)
		}
		batchTexts := allChunks[i:end]

		if embedder, ok := ragSvc.Embedder.(*einoembedding.Embedder); ok && embedder != nil {
			embeddings, err := embedder.EmbedStrings(ctx, batchTexts)
			if err != nil {
				fmt.Printf("⚠️  获取 Embedding 失败 (批次 %d-%d): %v\n", i, end, err)
				ragSvc.Store.AddBatch(batchTexts, nil)
				continue
			}
			ragSvc.Store.AddBatch(batchTexts, embeddings)
		} else {
			ragSvc.Store.AddBatch(batchTexts, nil)
		}
	}

	ragSvc.Store.BuildBM25Index()
	texts, _, _ := ragSvc.Store.Snapshot()
	fmt.Printf("✅ RAG 知识库构建完成，共索引 %d 个文本块（向量+BM25 混合检索）。\n", len(texts))
	return ragSvc, nil
}

func main() {
	flag.Parse()

	var c config.Config
	conf.MustLoad(*configFile, &c)

	// 初始化 RAG
	ragSvc, err := initRAG(context.Background(), c)
	if err != nil {
		fmt.Fprintf(os.Stderr, "RAG 初始化失败: %v\n", err)
		os.Exit(1)
	}

	ctx := svc.NewServiceContext(c, ragSvc)

	s := zrpc.MustNewServer(c.RpcServerConf, func(grpcServer *grpc.Server) {
		rag.RegisterRagServiceServer(grpcServer, server.NewRagServiceServer(ctx))

		if c.Mode == service.DevMode || c.Mode == service.TestMode {
			reflection.Register(grpcServer)
		}
	})
	defer s.Stop()

	fmt.Printf("Starting rpc server at %s...\n", c.ListenOn)
	s.Start()
}
