package store

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
)

// PgVectorOK 标记 pgvector 表是否可用
var PgVectorOK bool

const pgTableName = "rag_chunks"

// EnsurePgVectorTable 创建表和向量索引
func EnsurePgVectorTable(ctx context.Context, dim int) error {
	if DB == nil {
		return fmt.Errorf("PG 未连接")
	}

	_, _ = DB.Exec(ctx, fmt.Sprintf("DROP TABLE IF EXISTS %s", pgTableName))

	createSQL := fmt.Sprintf(`
		CREATE TABLE %s (
			id   SERIAL PRIMARY KEY,
			text TEXT NOT NULL,
			vec  vector(%d)
		)
	`, pgTableName, dim)
	if _, err := DB.Exec(ctx, createSQL); err != nil {
		return fmt.Errorf("建表失败: %v", err)
	}

	tryHNSW := []string{
		fmt.Sprintf("CREATE INDEX idx_%s_vec ON %s USING hnsw (vec halfvec_cosine_ops)", pgTableName, pgTableName),
		fmt.Sprintf("CREATE INDEX idx_%s_vec ON %s USING hnsw (vec vector_cosine_ops)", pgTableName, pgTableName),
	}
	indexed := false
	for _, sql := range tryHNSW {
		if _, err := DB.Exec(ctx, sql); err == nil {
			indexed = true
			break
		}
	}

	PgVectorOK = true
	if indexed {
		fmt.Printf("📦 PG 表 '%s' 创建完成，向量维度=%d，HNSW 索引已建立\n", pgTableName, dim)
	} else {
		fmt.Printf("📦 PG 表 '%s' 创建完成，向量维度=%d（顺序扫描）\n", pgTableName, dim)
	}
	return nil
}

// InsertPgVector 批量插入文本和向量
func InsertPgVector(ctx context.Context, texts []string, vectors [][]float64) error {
	if !PgVectorOK || DB == nil {
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

	br := DB.SendBatch(ctx, batch)
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
	if !PgVectorOK || DB == nil {
		return nil, nil
	}

	vecStr := vectorToPgStr(queryVec)
	sql := fmt.Sprintf(`
		SELECT id
		FROM %s
		ORDER BY vec <=> $1::vector
		LIMIT $2
	`, pgTableName)

	rows, err := DB.Query(ctx, sql, vecStr, topK)
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

// vectorToPgStr 把 []float64 转换为 pgvector 格式字符串
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
