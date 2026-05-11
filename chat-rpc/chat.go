package main

import (
	"context"
	"flag"
	"fmt"
	"net"
	"os"

	"awesomeProject/chat-rpc/awesomeProject/chat-rpc/chat"
	"awesomeProject/chat-rpc/internal/config"
	"awesomeProject/chat-rpc/internal/lib"
	"awesomeProject/chat-rpc/internal/server"
	"awesomeProject/chat-rpc/internal/svc"

	"github.com/zeromicro/go-zero/core/conf"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
)

var configFile = flag.String("f", "etc/chat.yaml", "the config file")

func main() {
	flag.Parse()

	var c config.Config
	conf.MustLoad(*configFile, &c)

	// 加载提示词配置
	promptCfg, err := lib.LoadPromptConfig(c.PromptConfig.Path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "提示词配置加载失败: %v\n", err)
		os.Exit(1)
	}

	// 初始化 Agent Registry
	agentReg, err := lib.NewRegistry(context.Background(), promptCfg, c.Models.KimiK2.APIKey, c.Models.GLM4.APIKey)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Agent Registry 初始化失败: %v\n", err)
		os.Exit(1)
	}

	ctx := svc.NewServiceContext(c, agentReg)

	// 使用原生 gRPC（不依赖 etcd）
	lis, err := net.Listen("tcp", c.ListenOn)
	if err != nil {
		fmt.Fprintf(os.Stderr, "监听失败: %v\n", err)
		os.Exit(1)
	}

	grpcServer := grpc.NewServer()
	chat.RegisterChatServiceServer(grpcServer, server.NewChatServiceServer(ctx))
	reflection.Register(grpcServer)

	fmt.Printf("Starting rpc server at %s...\n", c.ListenOn)
	if err := grpcServer.Serve(lis); err != nil {
		fmt.Fprintf(os.Stderr, "服务启动失败: %v\n", err)
		os.Exit(1)
	}
}
