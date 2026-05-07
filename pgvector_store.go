package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	pgDB       *pgxpool.Pool
	pgvectorOK bool
)

const pgTableName = "rag_chunks"

// InitPgVector 连接 PostgreSQL + pgvector
func InitPgVector(ctx context.Context, dsn string) error {
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
	pgDB = pool

	// 启用 pgvector 扩展
	if _, err := pgDB.Exec(ctx, "CREATE EXTENSION IF NOT EXISTS vector"); err != nil {
		return fmt.Errorf("创建 vector 扩展失败: %v", err)
	}

	fmt.Println("✅ 已连接 PostgreSQL + pgvector")
	return nil
}

// EnsurePgVectorTable 创建表和 HNSW 向量索引
func EnsurePgVectorTable(ctx context.Context, dim int) error {
	if pgDB == nil {
		return fmt.Errorf("PG 未连接")
	}

	_, _ = pgDB.Exec(ctx, fmt.Sprintf("DROP TABLE IF EXISTS %s", pgTableName))

	// vector 类型支持任意维度存储（pgvector 0.5+）
	createSQL := fmt.Sprintf(`
		CREATE TABLE %s (
			id   SERIAL PRIMARY KEY,
			text TEXT NOT NULL,
			vec  vector(%d)
		)
	`, pgTableName, dim)
	if _, err := pgDB.Exec(ctx, createSQL); err != nil {
		return fmt.Errorf("建表失败: %v", err)
	}

	// 尝试建 HNSW 索引（pgvector ≥0.7 halfvec 上限 4000 维；旧版超出 2000 维时跳过，降级顺序扫描）
	tryHNSW := []string{
		fmt.Sprintf("CREATE INDEX idx_%s_vec ON %s USING hnsw (vec halfvec_cosine_ops)", pgTableName, pgTableName),
		fmt.Sprintf("CREATE INDEX idx_%s_vec ON %s USING hnsw (vec vector_cosine_ops)", pgTableName, pgTableName),
	}
	indexed := false
	for _, sql := range tryHNSW {
		if _, err := pgDB.Exec(ctx, sql); err == nil {
			indexed = true
			break
		}
	}

	pgvectorOK = true
	if indexed {
		fmt.Printf("📦 PG 表 '%s' 创建完成，向量维度=%d，HNSW 索引已建立\n", pgTableName, dim)
	} else {
		fmt.Printf("📦 PG 表 '%s' 创建完成，向量维度=%d（顺序扫描，升级 pgvector ≥0.7 可启用 HNSW 索引）\n", pgTableName, dim)
	}
	return nil
}

// InsertPgVector 批量插入文本和向量
func InsertPgVector(ctx context.Context, texts []string, vectors [][]float64) error {
	if !pgvectorOK || pgDB == nil {
		return nil
	}

	batch := &pgx.Batch{}
	for i, text := range texts {
		vecStr := vectorToPgStr(vectors[i])
		batch.Queue(
			fmt.Sprintf("INSERT INTO %s (text, vec) VALUES ($1, $2::vector)", pgTableName),
			text, vecStr,
		)
	}

	br := pgDB.SendBatch(ctx, batch)
	defer br.Close()

	for range texts {
		if _, err := br.Exec(); err != nil {
			return fmt.Errorf("批量插入失败: %v", err)
		}
	}
	return nil
}

// SearchPgVector 向量相似度检索，返回 id 列表（id 从 1 开始）
func SearchPgVector(ctx context.Context, queryVec []float64, topK int) ([]int, error) {
	if !pgvectorOK || pgDB == nil {
		return nil, nil
	}

	vecStr := vectorToPgStr(queryVec)
	sql := fmt.Sprintf(`
		SELECT id
		FROM %s
		ORDER BY vec <=> $1::vector
		LIMIT $2
	`, pgTableName)

	rows, err := pgDB.Query(ctx, sql, vecStr, topK)
	if err != nil {
		return nil, fmt.Errorf("PG 向量搜索失败: %v", err)
	}
	defer rows.Close()

	var ids []int
	for rows.Next() {
		var id int
		if err := rows.Scan(&id); err != nil {
			continue
		}
		ids = append(ids, id)
	}
	return ids, nil
}

// vectorToPgStr 把 []float64 转换为 pgvector 格式字符串，例如 '[1.0,2.0,3.0]'
func vectorToPgStr(vec []float64) string {
	var b strings.Builder
	b.WriteString("[")
	for i, v := range vec {
		if i > 0 {
			b.WriteString(",")
		}
		fmt.Fprintf(&b, "%f", v)
	}
	b.WriteString("]")
	return b.String()
}
