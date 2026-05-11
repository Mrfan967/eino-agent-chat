package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"awesomeProject/session-rpc/awesomeProject/session-rpc/session"
	"awesomeProject/session-rpc/internal/config"
	"awesomeProject/session-rpc/internal/server"
	"awesomeProject/session-rpc/internal/svc"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/zeromicro/go-zero/core/conf"
	"github.com/zeromicro/go-zero/core/service"
	"github.com/zeromicro/go-zero/zrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
)

var configFile = flag.String("f", "etc/session.yaml", "the config file")

func initDB(ctx context.Context, dsn string) (*pgxpool.Pool, error) {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("连接 PG 失败: %v", err)
	}
	if err := pool.Ping(ctx); err != nil {
		return nil, fmt.Errorf("PG Ping 失败: %v", err)
	}

	// 创建表
	_, err = pool.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS chat_sessions (
			id         SERIAL PRIMARY KEY,
			session_id VARCHAR(64) NOT NULL UNIQUE,
			created_at TIMESTAMP DEFAULT NOW(),
			updated_at TIMESTAMP DEFAULT NOW()
		)
	`)
	if err != nil {
		return nil, fmt.Errorf("创建 chat_sessions 失败: %v", err)
	}

	_, err = pool.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS chat_messages (
			id         SERIAL PRIMARY KEY,
			session_id VARCHAR(64) NOT NULL,
			role       VARCHAR(16) NOT NULL,
			content    TEXT NOT NULL,
			created_at TIMESTAMP DEFAULT NOW()
		)
	`)
	if err != nil {
		return nil, fmt.Errorf("创建 chat_messages 失败: %v", err)
	}

	return pool, nil
}

func main() {
	flag.Parse()

	var c config.Config
	conf.MustLoad(*configFile, &c)

	// 初始化数据库
	db, err := initDB(context.Background(), c.Database.DSN)
	if err != nil {
		fmt.Fprintf(os.Stderr, "数据库初始化失败: %v\n", err)
		os.Exit(1)
	}
	defer db.Close()

	ctx := svc.NewServiceContext(c, db)

	s := zrpc.MustNewServer(c.RpcServerConf, func(grpcServer *grpc.Server) {
		session.RegisterSessionServiceServer(grpcServer, server.NewSessionServiceServer(ctx))

		if c.Mode == service.DevMode || c.Mode == service.TestMode {
			reflection.Register(grpcServer)
		}
	})
	defer s.Stop()

	fmt.Printf("Starting rpc server at %s...\n", c.ListenOn)
	s.Start()
}
