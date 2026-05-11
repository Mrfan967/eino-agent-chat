# Eino Agent Chat

> 基于 [CloudWeGo Eino](https://github.com/cloudwego/eino) + [go-zero](https://github.com/zeromicro/go-zero) 构建的微服务化智能对话助手，支持多模型切换、工具调用（Tool Calling）、RAG 知识库检索和会话持久化。

## ✨ 特性

- 🤖 **多模型支持**：Kimi (Moonshot)、智谱 GLM-4，一键切换
- 🛠️ **工具调用**：内置计算器、时间查询、美股行情（实时 + 历史 K 线）
- 📚 **RAG 检索**：向量检索 + BM25 混合检索，RRF 融合排序
- 💬 **流式对话**：WebSocket + gRPC Stream 双向流式输出
- 🗃️ **会话持久化**：PostgreSQL 存储多轮会话历史
- 🐳 **Docker 部署**：一键 `docker compose up`
- 🏗️ **微服务架构**：API 网关 + 三个 RPC 服务，职责清晰

## 🏛️ 架构

```
┌──────────────┐     WebSocket / HTTP      ┌───────────────┐
│   Web UI     │ ───────────────────────▶  │  api-gateway  │  :8080
└──────────────┘                           └───────┬───────┘
                                                   │ gRPC
                          ┌────────────────────────┼────────────────────────┐
                          ▼                        ▼                        ▼
                  ┌───────────────┐       ┌───────────────┐       ┌───────────────┐
                  │   chat-rpc    │       │   rag-rpc     │       │ session-rpc   │
                  │     :8083     │       │     :8082     │       │     :8081     │
                  │  Eino Agent   │       │ 向量+BM25检索 │       │  会话管理     │
                  └───────────────┘       └───────────────┘       └───────┬───────┘
                                                                          │
                                                                          ▼
                                                                  ┌───────────────┐
                                                                  │  PostgreSQL   │
                                                                  │  + pgvector   │
                                                                  └───────────────┘
```

## 📁 目录结构

```
awesomeProject/
├── api-gateway/        # HTTP/WebSocket 网关
├── chat-rpc/           # 对话服务（Eino Agent + 工具调用）
├── rag-rpc/            # RAG 检索服务（向量 + BM25）
├── session-rpc/        # 会话存储服务（PostgreSQL）
├── knowledge/          # RAG 知识库 .txt 文件
├── web/                # 前端页面
├── image/              # 静态图片资源
├── prompt_config.json  # 提示词配置
├── docker-compose.yml  # Docker 编排
└── Dockerfile          # 镜像构建
```

## 🚀 快速开始

### 方式一：Docker Compose（推荐）

```bash
# 1. 配置环境变量（可选，默认已有内置 key）
export MOONSHOT_API_KEY=your_moonshot_key
export ZHIPU_API_KEY=your_zhipu_key

# 2. 一键启动
docker compose up -d

# 3. 访问
open http://localhost:8080
```

### 方式二：本地启动

**前置依赖**

- Go 1.24+
- PostgreSQL 16+（启用 pgvector 扩展）
- Redis（可选）

**启动 PostgreSQL**

```bash
docker run -d --name eino-postgres \
  -e POSTGRES_PASSWORD=postgres \
  -p 5432:5432 \
  pgvector/pgvector:pg16
```

**依次启动四个服务**

```bash
# Windows
start.bat

# Linux / macOS
chmod +x start.sh && ./start.sh
```

或手动启动：

```bash
cd session-rpc && go run session.go -f etc/session.yaml &
cd rag-rpc     && go run rag.go     -f etc/rag.yaml     &
cd chat-rpc    && go run chat.go    -f etc/chat.yaml    &
cd api-gateway && go run gateway.go -f etc/gateway.yaml &
```

## 🔧 配置说明

### 模型 API Key

在 `chat-rpc/etc/chat.yaml` 中配置：

```yaml
Models:
  KimiK2:
    APIKey: "sk-xxx"
    BaseURL: "https://api.moonshot.cn/v1"
    Model: "moonshot-v1-8k"
  GLM4:
    APIKey: "xxx"
    BaseURL: "https://open.bigmodel.cn/api/paas/v4"
    Model: "glm-4.1v-thinking-flash"
```

### 提示词与触发规则

编辑 `prompt_config.json`：

```json
{
  "persona": "你是一个智能助手...",
  "system_rules": ["规则1", "规则2"],
  "rules": [
    {
      "trigger": "你是谁",
      "answer": "我是 Eino 智能助手",
      "image": "logo.png"
    }
  ],
  "fallback": "抱歉，我暂时无法回答这个问题"
}
```

### RAG 知识库

把 `.txt` 文件放进 `knowledge/` 目录，启动 `rag-rpc` 时会自动分块、生成向量并构建 BM25 索引。

## 🛠️ 内置工具

| 工具 | 说明 |
|------|------|
| `calculator` | 加减乘除四则运算 |
| `get_current_time` | 当前时间 |
| `stock_query` | 美股实时行情 + 近 N 天 K 线历史（新浪 + 东方财富，国内可访问） |

## 📡 API

### WebSocket 流式聊天

```
ws://localhost:8080/ws/chat
```

**发送消息**

```json
{
  "session_id": "session-001",
  "model": "kimi-k2",
  "message": "苹果近七天的股价"
}
```

**接收响应（流式）**

```json
{"content": "苹果（AAPL）..."}
{"content": "近七天..."}
{"image": "/image/apple.png"}
{"done": true}
```

### HTTP 接口

| 方法 | 路径 | 说明 |
|------|------|------|
| POST | `/api/chat/clear` | 清空指定会话 |
| GET  | `/api/chat/history?session_id=xxx` | 获取会话历史 |
| GET  | `/image/:file` | 静态图片资源 |

## 🧩 技术栈

- **后端框架**：[go-zero](https://github.com/zeromicro/go-zero)
- **Agent 框架**：[CloudWeGo Eino](https://github.com/cloudwego/eino)
- **RPC**：gRPC
- **数据库**：PostgreSQL + pgvector
- **前端**：原生 HTML/JS + WebSocket

## 📜 License

MIT

## 🙌 致谢

- [CloudWeGo Eino](https://github.com/cloudwego/eino)
- [go-zero](https://github.com/zeromicro/go-zero)
- [新浪财经 API](https://hq.sinajs.cn) / [东方财富 API](https://push2his.eastmoney.com)
