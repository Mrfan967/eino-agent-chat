package store

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// DB 全局 PostgreSQL 连接池
var DB *pgxpool.Pool

// InitDB 连接 PostgreSQL 并启用 pgvector 扩展
func InitDB(ctx context.Context, dsn string) error {
	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return fmt.Errorf("解析 DSN 失败: %v", err)
	}

	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return fmt.Errorf("连接 PG 失败: %v", err)
	}

	if err := pool.Ping(ctx); err != nil {
		return fmt.Errorf("PG Ping 失败: %v", err)
	}
	DB = pool

	// 启用 pgvector 扩展
	if _, err := DB.Exec(ctx, "CREATE EXTENSION IF NOT EXISTS vector"); err != nil {
		return fmt.Errorf("创建 vector 扩展失败: %v", err)
	}

	fmt.Println("✅ 已连接 PostgreSQL + pgvector")
	return nil
}
