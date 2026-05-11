package handler

import (
	"encoding/json"
	"io"
	"net/http"

	chat "awesomeProject/api-gateway/internal/proto"
	"awesomeProject/api-gateway/internal/svc"

	"github.com/gorilla/websocket"
	"github.com/zeromicro/go-zero/core/logx"
)

// WSMessage WebSocket 消息结构
type WSMessage struct {
	Message   string `json:"message"`
	Model     string `json:"model"`
	SessionID string `json:"session_id"`
}

// WSResponse WebSocket 响应
type WSResponse struct {
	Content string `json:"content,omitempty"`
	Done    bool   `json:"done,omitempty"`
	Error   string `json:"error,omitempty"`
	Image   string `json:"image,omitempty"`
}

// WebSocketHandler WebSocket 聊天 handler
func WebSocketHandler(ctx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			logx.Error("WebSocket 升级失败:", err)
			return
		}
		defer conn.Close()

		for {
			_, msgBytes, err := conn.ReadMessage()
			if err != nil {
				if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure) {
					logx.Error("WebSocket 异常断开:", err)
				}
				break
			}

			var req WSMessage
			if err := json.Unmarshal(msgBytes, &req); err != nil {
				resp := WSResponse{Error: "请求格式错误"}
				conn.WriteJSON(resp)
				continue
			}

			if req.Message == "" {
				resp := WSResponse{Error: "消息不能为空"}
				conn.WriteJSON(resp)
				continue
			}

			// 调用 chat-rpc 流式接口
			if ctx.ChatCli == nil {
				resp := WSResponse{Error: "chat-rpc 客户端未初始化"}
				conn.WriteJSON(resp)
				conn.WriteJSON(WSResponse{Done: true})
				continue
			}

			// 建立 gRPC 流式连接
			stream, err := ctx.ChatCli.Stream(r.Context())
			if err != nil {
				logx.Error("建立 chat-rpc Stream 失败:", err)
				resp := WSResponse{Error: "连接 chat-rpc 失败: " + err.Error()}
				conn.WriteJSON(resp)
				conn.WriteJSON(WSResponse{Done: true})
				continue
			}

			// 发送请求
			model := req.Model
			if model == "" {
				model = "kimi-k2"
			}
			grpcReq := &chat.ChatReq{
				SessionId: req.SessionID,
				Model:     model,
				Message:   req.Message,
			}
			if err := stream.Send(grpcReq); err != nil {
				logx.Error("发送消息到 chat-rpc 失败:", err)
				resp := WSResponse{Error: "发送消息失败: " + err.Error()}
				conn.WriteJSON(resp)
				conn.WriteJSON(WSResponse{Done: true})
				continue
			}

			// 接收流式响应
			for {
				chunk, err := stream.Recv()
				if err == io.EOF {
					break
				}
				if err != nil {
					logx.Error("接收 chat-rpc 响应失败:", err)
					resp := WSResponse{Error: "接收响应失败: " + err.Error()}
					conn.WriteJSON(resp)
					break
				}

				// 转换为 WebSocket 响应
				wsResp := WSResponse{
					Content: chunk.Content,
					Done:    chunk.Done,
					Error:   chunk.Error,
					Image:   chunk.Image,
				}
				if err := conn.WriteJSON(wsResp); err != nil {
					logx.Error("WebSocket 发送失败:", err)
					break
				}

				if chunk.Done {
					break
				}
			}
		}
	}
}
