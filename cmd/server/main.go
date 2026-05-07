package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"runtime"

	"awesomeProject/internal/agent"
	"awesomeProject/internal/config"
	"awesomeProject/internal/handler"
	"awesomeProject/internal/rag"
	"awesomeProject/internal/store"
)

func main() {
	ctx := context.Background()

	// 1. 连接 PostgreSQL（可选）
	if dsn := os.Getenv("PGVECTOR_DSN"); dsn != "" {
		if err := store.InitDB(ctx, dsn); err != nil {
			fmt.Printf("⚠️  pgvector 连接失败: %v，将仅使用内存向量+BM25。\n", err)
		}
	}

	// 2. 初始化会话表
	if store.DB != nil {
		if err := store.EnsureSessionTables(ctx); err != nil {
			fmt.Printf("⚠️  会话表初始化失败: %v，将仅使用内存历史。\n", err)
		}
	}

	// 3. 加载提示词配置
	cfg, err := config.Load("prompt_config.json")
	if err != nil {
		fmt.Printf("⚠️  读取 prompt_config.json 失败: %v，将使用默认提示\n", err)
		cfg = config.Default()
	}

	// 4. 初始化 RAG
	apiKey := os.Getenv("ZHIPU_API_KEY")
	if apiKey == "" {
		apiKey = os.Getenv("OPENAI_API_KEY")
	}
	ragSvc, err := rag.NewService(ctx, apiKey)
	if err != nil {
		fmt.Printf("⚠️  RAG 知识库初始化失败: %v\n", err)
		ragSvc, _ = rag.NewService(ctx, "") // 降级为纯 BM25
	} else {
		fmt.Println("✅ RAG 知识库初始化成功 (如果 knowledge 目录下有文件)")
	}

	// 5. 初始化 Agent 注册表
	apiKeys := map[string]string{
		"MOONSHOT_API_KEY": os.Getenv("MOONSHOT_API_KEY"),
		"ZHIPU_API_KEY":    os.Getenv("ZHIPU_API_KEY"),
		"OPENAI_API_KEY":   os.Getenv("OPENAI_API_KEY"),
	}
	registry, err := agent.NewRegistry(ctx, ragSvc, cfg, apiKeys)
	if err != nil {
		fmt.Printf("初始化 Agents 失败: %v\n", err)
		return
	}

	// 6. 注册 HTTP 路由
	chatHandler := handler.NewChatHandler(registry, cfg)
	chatHandler.RegisterRoutes()

	// 7. 启动服务
	addr := ":8080"
	fmt.Println("\n🌐 Web 对话服务已启动！(支持多模型 + 上下文记忆)")
	fmt.Println("   浏览器访问: http://localhost" + addr)
	fmt.Println("   按 Ctrl+C 停止服务\n")

	openBrowser("http://localhost" + addr)

	if err := http.ListenAndServe(addr, nil); err != nil {
		fmt.Printf("服务启动失败: %v\n", err)
	}
}

func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	case "darwin":
		cmd = exec.Command("open", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	cmd.Start()
}
