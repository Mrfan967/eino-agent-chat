package main

import (
	"flag"
	"fmt"
	"os"

	"awesomeProject/api-gateway/internal/client"
	"awesomeProject/api-gateway/internal/config"
	"awesomeProject/api-gateway/internal/handler"
	"awesomeProject/api-gateway/internal/svc"

	"github.com/zeromicro/go-zero/core/conf"
	"github.com/zeromicro/go-zero/rest"
)

var configFile = flag.String("f", "etc/gateway.yaml", "the config file")

func main() {
	flag.Parse()

	var c config.Config
	conf.MustLoad(*configFile, &c)

	// 创建 chat-rpc 客户端
	chatCli, err := client.NewChatClient(c.ChatRpc.Endpoints)
	if err != nil {
		fmt.Fprintf(os.Stderr, "chat-rpc 客户端创建失败: %v\n", err)
		// 暂时不退出，允许测试模式运行
		chatCli = nil
	}

	ctx := svc.NewServiceContext(c, chatCli)

	server := rest.MustNewServer(c.RestConf)
	defer server.Stop()

	// 注册路由
	handler.RegisterHandlers(server, ctx)

	fmt.Printf("Starting api-gateway at %s...\n", c.Port)
	server.Start()
}
