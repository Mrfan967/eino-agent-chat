package handler

import (
	"encoding/json"
	"net/http"
	"os"

	"awesomeProject/api-gateway/internal/svc"

	"github.com/zeromicro/go-zero/core/logx"
)

// ClearHandler 清空会话 handler
func ClearHandler(ctx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			SessionID string `json:"session_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "请求格式错误", http.StatusBadRequest)
			return
		}

		// TODO: 调用 session-rpc 删除会话
		logx.Infof("清空会话: %s", req.SessionID)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]bool{"success": true})
	}
}

// HistoryHandler 获取历史消息 handler
func HistoryHandler(ctx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		sessionID := r.URL.Query().Get("session_id")
		if sessionID == "" {
			sessionID = "default"
		}

		// TODO: 调用 session-rpc 加载历史
		logx.Infof("加载历史: %s", sessionID)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"session_id": sessionID,
			"messages":   []map[string]string{},
		})
	}
}

// IndexHandler 首页 handler
func IndexHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// 读取 web/chat.html
		data, err := os.ReadFile("../web/chat.html")
		if err != nil {
			http.Error(w, "页面未找到", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(data)
	}
}

// ImageHandler 图片资源 handler
func ImageHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		file := r.PathValue("file")
		http.ServeFile(w, r, "../image/"+file)
	}
}
