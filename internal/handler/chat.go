package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/cloudwego/eino/schema"

	"awesomeProject/internal/agent"
	"awesomeProject/internal/config"
	"awesomeProject/internal/store"
)

var chatHTML []byte

func init() {
	data, err := os.ReadFile("web/chat.html")
	if err != nil {
		panic("无法读取 web/chat.html: " + err.Error())
	}
	chatHTML = data
}

// streamChunk SSE 单个数据块
type streamChunk struct {
	Content string `json:"content,omitempty"`
	Error   string `json:"error,omitempty"`
	Done    bool   `json:"done,omitempty"`
	Image   string `json:"image,omitempty"`
}

// ChatHandler 持有聊天处理所需的全部依赖
type ChatHandler struct {
	registry         *agent.Registry
	cfg              *config.PromptConfig
	sessionHistories map[string][]*schema.Message
	mu               sync.Mutex
}

// NewChatHandler 创建 ChatHandler
func NewChatHandler(registry *agent.Registry, cfg *config.PromptConfig) *ChatHandler {
	return &ChatHandler{
		registry:         registry,
		cfg:              cfg,
		sessionHistories: make(map[string][]*schema.Message),
	}
}

// RegisterRoutes 注册所有 HTTP 路由
func (h *ChatHandler) RegisterRoutes() {
	http.HandleFunc("/api/chat/stream", h.handleStream)
	http.HandleFunc("/api/chat/clear", h.handleClear)
	http.HandleFunc("/api/chat/history", h.handleHistory)
	http.Handle("/image/", http.StripPrefix("/image/", http.FileServer(http.Dir("image"))))
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(chatHTML)
	})
}

// handleStream 处理 SSE 流式聊天
func (h *ChatHandler) handleStream(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Message   string `json:"message"`
		Model     string `json:"model"`
		SessionID string `json:"session_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "请求格式错误", http.StatusBadRequest)
		return
	}
	if req.Message == "" {
		http.Error(w, "消息不能为空", http.StatusBadRequest)
		return
	}

	modelID := req.Model
	if modelID == "" {
		modelID = "kimi-k2"
	}

	ag, ok := h.registry.Get(modelID)
	if !ok {
		http.Error(w, "不支持的模型", http.StatusBadRequest)
		return
	}

	start := time.Now()

	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "不支持流式输出", http.StatusInternalServerError)
		return
	}

	sid := req.SessionID
	if sid == "" {
		sid = "default"
	}

	log.Printf("[INFO] 收到消息 session=%s model=%s msg=%.60s", sid, modelID, req.Message)

	userMsg := schema.UserMessage(req.Message)

	h.mu.Lock()
	if _, exists := h.sessionHistories[sid]; !exists {
		if loaded, loadErr := store.LoadSessionHistory(r.Context(), sid); loadErr == nil && len(loaded) > 0 {
			h.sessionHistories[sid] = loaded
		}
	}
	history := h.sessionHistories[sid]
	history = append(history, userMsg)
	h.sessionHistories[sid] = history
	messages := make([]*schema.Message, len(history))
	copy(messages, history)
	h.mu.Unlock()

	go func() {
		if err := store.SaveMessage(context.Background(), sid, "user", req.Message); err != nil {
			log.Printf("[WARN] 保存用户消息失败 session=%s: %v", sid, err)
		}
	}()

	streamResult, err := ag.Stream(r.Context(), messages)
	if err != nil {
		log.Printf("[ERROR] Agent 出错 session=%s model=%s: %v", sid, modelID, err)
		chunk, _ := json.Marshal(streamChunk{Error: fmt.Sprintf("Agent 出错: %v", err)})
		fmt.Fprintf(w, "data: %s\n\n", chunk)
		flusher.Flush()
		fmt.Fprintf(w, "data: [DONE]\n\n")
		flusher.Flush()

		h.mu.Lock()
		if hist, ok := h.sessionHistories[sid]; ok && len(hist) > 0 {
			h.sessionHistories[sid] = hist[:len(hist)-1]
		}
		h.mu.Unlock()
		return
	}

	var fullContent strings.Builder
	for {
		msg, err := streamResult.Recv()
		if err != nil {
			break
		}
		if msg == nil || msg.Content == "" {
			continue
		}
		fullContent.WriteString(msg.Content)
		chunk, _ := json.Marshal(streamChunk{Content: msg.Content})
		fmt.Fprintf(w, "data: %s\n\n", chunk)
		flusher.Flush()
	}

	finalReply := fullContent.String()
	if finalReply != "" {
		h.mu.Lock()
		h.sessionHistories[sid] = append(h.sessionHistories[sid], schema.AssistantMessage(finalReply, nil))
		h.mu.Unlock()

		log.Printf("[INFO] 回复完成 session=%s model=%s 长度=%d 耗时=%v", sid, modelID, len(finalReply), time.Since(start))
		go func(reply string) {
			if err := store.SaveMessage(context.Background(), sid, "assistant", reply); err != nil {
				log.Printf("[WARN] 保存 AI 回复失败 session=%s: %v", sid, err)
			}
		}(finalReply)
	}

	imageURL := matchImage(h.cfg, req.Message, finalReply)
	if imageURL != "" {
		chunk, _ := json.Marshal(streamChunk{Image: imageURL})
		fmt.Fprintf(w, "data: %s\n\n", chunk)
		flusher.Flush()
	}

	fmt.Fprintf(w, "data: [DONE]\n\n")
	flusher.Flush()
}

// handleClear 清空会话历史
func (h *ChatHandler) handleClear(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		SessionID string `json:"session_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "请求格式错误", http.StatusBadRequest)
		return
	}

	h.mu.Lock()
	delete(h.sessionHistories, req.SessionID)
	h.mu.Unlock()

	log.Printf("[INFO] 清空会话 session=%s", req.SessionID)
	go func(sid string) {
		if err := store.DeleteSession(context.Background(), sid); err != nil {
			log.Printf("[WARN] 删除 PG 会话失败 session=%s: %v", sid, err)
		}
	}(req.SessionID)

	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{"status":"ok"}`))
}

// handleHistory 返回指定 session 的历史消息
func (h *ChatHandler) handleHistory(w http.ResponseWriter, r *http.Request) {
	sid := r.URL.Query().Get("session_id")
	if sid == "" {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"messages":[]}`))
		return
	}

	msgs, err := store.LoadSessionHistory(r.Context(), sid)
	if err != nil || len(msgs) == 0 {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"messages":[]}`))
		return
	}

	type historyMsg struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}
	var result []historyMsg
	for _, m := range msgs {
		role := "user"
		if m.Role == schema.Assistant {
			role = "assistant"
		} else if m.Role == schema.System {
			role = "system"
		}
		result = append(result, historyMsg{Role: role, Content: m.Content})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"messages": result})
}

// matchImage 根据消息内容匹配图片
func matchImage(cfg *config.PromptConfig, userMsg, reply string) string {
	if cfg == nil {
		return ""
	}
	msgLower := strings.ToLower(userMsg)
	for _, r := range cfg.Rules {
		triggerLower := strings.ToLower(r.Trigger)
		if r.Image != "" && (strings.Contains(msgLower, triggerLower) || strings.Contains(triggerLower, msgLower)) {
			return "/image/" + r.Image
		}
	}
	replyLower := strings.ToLower(reply)
	if strings.Contains(replyLower, "赵苏通") {
		return "/image/su.jpg"
	} else if strings.Contains(replyLower, "范晨旭") {
		return "/image/fan.jpg"
	}
	return ""
}
