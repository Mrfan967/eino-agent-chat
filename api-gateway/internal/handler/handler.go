package handler

import (
	"net/http"

	"awesomeProject/api-gateway/internal/svc"

	"github.com/gorilla/websocket"
	"github.com/zeromicro/go-zero/rest"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

// RegisterHandlers 注册所有 handler
func RegisterHandlers(server *rest.Server, ctx *svc.ServiceContext) {
	// WebSocket 聊天
	server.AddRoute(rest.Route{
		Method:  http.MethodGet,
		Path:    "/ws/chat",
		Handler: WebSocketHandler(ctx),
	})

	// HTTP API（兼容旧接口）
	server.AddRoute(rest.Route{
		Method:  http.MethodPost,
		Path:    "/api/chat/clear",
		Handler: ClearHandler(ctx),
	})

	server.AddRoute(rest.Route{
		Method:  http.MethodGet,
		Path:    "/api/chat/history",
		Handler: HistoryHandler(ctx),
	})

	// 静态资源
	server.AddRoute(rest.Route{
		Method:  http.MethodGet,
		Path:    "/",
		Handler: IndexHandler(),
	})

	server.AddRoute(rest.Route{
		Method:  http.MethodGet,
		Path:    "/image/:file",
		Handler: ImageHandler(),
	})
}
