package store

import (
	"context"
	"fmt"
	"time"

	"github.com/cloudwego/eino/schema"
)

// EnsureSessionTables 确保会话表存在
func EnsureSessionTables(ctx context.Context) error {
	if DB == nil {
		return nil
	}

	_, err := DB.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS chat_sessions (
			id         SERIAL PRIMARY KEY,
			session_id VARCHAR(64) NOT NULL UNIQUE,
			created_at TIMESTAMP DEFAULT NOW(),
			updated_at TIMESTAMP DEFAULT NOW()
		)
	`)
	if err != nil {
		return fmt.Errorf("创建 chat_sessions 失败: %v", err)
	}

	_, err = DB.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS chat_messages (
			id         SERIAL PRIMARY KEY,
			session_id VARCHAR(64) NOT NULL,
			role       VARCHAR(16) NOT NULL,
			content    TEXT NOT NULL,
			created_at TIMESTAMP DEFAULT NOW(),
			CONSTRAINT fk_chat_messages_session
				FOREIGN KEY (session_id) REFERENCES chat_sessions(session_id)
				ON DELETE CASCADE
		)
	`)
	if err != nil {
		return fmt.Errorf("创建 chat_messages 失败: %v", err)
	}

	fmt.Println("✅ 会话表已就绪（chat_sessions + chat_messages）")
	return nil
}

// LoadSessionHistory 从 PG 加载指定 session 的历史消息
func LoadSessionHistory(ctx context.Context, sessionID string) ([]*schema.Message, error) {
	if DB == nil {
		return nil, nil
	}

	rows, err := DB.Query(ctx, `
		SELECT role, content FROM chat_messages
		WHERE session_id = $1
		ORDER BY created_at ASC
	`, sessionID)
	if err != nil {
		return nil, fmt.Errorf("加载会话历史失败: %v", err)
	}
	defer rows.Close()

	var messages []*schema.Message
	for rows.Next() {
		var role, content string
		if err := rows.Scan(&role, &content); err != nil {
			continue
		}
		switch role {
		case "user":
			messages = append(messages, schema.UserMessage(content))
		case "assistant":
			messages = append(messages, schema.AssistantMessage(content, nil))
		case "system":
			messages = append(messages, schema.SystemMessage(content))
		}
	}
	return messages, nil
}

// SaveMessage 保存单条消息到 PG
func SaveMessage(ctx context.Context, sessionID, role, content string) error {
	if DB == nil {
		return nil
	}

	_, err := DB.Exec(ctx, `
		INSERT INTO chat_sessions (session_id, updated_at)
		VALUES ($1, $2)
		ON CONFLICT (session_id) DO UPDATE SET updated_at = $2
	`, sessionID, time.Now())
	if err != nil {
		return fmt.Errorf("upsert session 失败: %v", err)
	}

	_, err = DB.Exec(ctx, `
		INSERT INTO chat_messages (session_id, role, content)
		VALUES ($1, $2, $3)
	`, sessionID, role, content)
	if err != nil {
		return fmt.Errorf("保存消息失败: %v", err)
	}
	return nil
}

// DeleteSession 删除指定 session 的所有历史
func DeleteSession(ctx context.Context, sessionID string) error {
	if DB == nil {
		return nil
	}
	_, err := DB.Exec(ctx, `DELETE FROM chat_sessions WHERE session_id = $1`, sessionID)
	if err != nil {
		return fmt.Errorf("删除会话失败: %v", err)
	}
	return nil
}
