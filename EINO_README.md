# Eino 框架入门示例

这是一个 Eino 框架的完整入门示例项目，展示了从基础用法到高级 Agent 的各种功能。

## 📦 项目结构

```
awesomeProject/
├── go.mod              # 依赖管理
├── heelo.go            # Go 基础语法示例
├── eino_basic.go       # Eino 基础示例
├── eino_agent.go       # Eino Agent 示例
├── main_eino.go        # Eino 示例主入口
└── EINO_README.md      # 本文件
```

## 🚀 快速开始

### 1. 安装依赖

```bash
go mod tidy
```

### 2. 设置 API Key

**Windows:**
```cmd
set OPENAI_API_KEY=your-api-key-here
```

**Linux/Mac:**
```bash
export OPENAI_API_KEY='your-api-key-here'
```

### 3. 运行示例

```bash
# 运行 Eino 示例
go run main_eino.go eino_basic.go eino_agent.go

# 或者运行 Go 基础语法示例
go run heelo.go
```

## 📚 示例说明

### 1. `eino_basic.go` - 基础示例

- ✅ **BasicChatModelExample**: ChatModel 的基本使用
  - 同步调用
  - 流式调用
  - 消息构建

- ✅ **ChainExample**: Chain 编排示例
  - Lambda 节点
  - ChatModel 节点
  - 数据流转

### 2. `eino_agent.go` - Agent 示例

- ✅ **ReActAgentExample**: ReAct Agent 实现
  - 工具定义（计算器、时间工具）
  - Graph 编排
  - 自主决策和工具调用

## 🔧 支持的模型

除了 OpenAI，Eino 还支持：

- **豆包（Doubao/Ark）**: `github.com/cloudwego/eino-ext/components/model/ark`
- **Claude**: `github.com/cloudwego/eino-ext/components/model/anthropic`
- **Gemini**: `github.com/cloudwego/eino-ext/components/model/gemini`
- **本地模型**: 通过 Ollama 等

## 📖 核心概念

### 组件（Components）
- **ChatModel**: LLM 对话模型（`github.com/cloudwego/eino/components/model`）
- **Retriever**: 检索器（RAG）
- **Tools**: 工具/函数调用（`github.com/cloudwego/eino/components/tool`）
- **Document Loader**: 文档加载器

### 编排（Orchestration）- **使用 `github.com/cloudwego/eino/compose` 包**
- **Chain**: 链式 DAG（`compose.NewChain`）
- **Graph**: 有向图（`compose.NewGraph`）
- **Workflow**: 带字段映射的 DAG

### 流式处理（Streaming）
- 支持流式输入输出
- 自动流包装
- 流分支判断

## 🌟 进阶功能

### 1. 自定义工具

```go
type MyTool struct{}

func (t *MyTool) Info(ctx context.Context) (*schema.ToolInfo, error) {
    // 返回工具信息
}

func (t *MyTool) InvokableRun(ctx context.Context, params map[string]interface{}) (string, error) {
    // 工具逻辑
}
```

### 2. Callback 机制

```go
// 添加回调监控执行过程
import "github.com/cloudwego/eino/compose"

chain := compose.NewChain[Input, Output]()
runnable, _ := chain.Compile(ctx, 
    compose.WithCallback(myCallback),
)
```

### 3. 状态管理

```go
// Graph 中使用状态
import "github.com/cloudwego/eino/compose"

graph := compose.NewGraph[Input, Output](
    compose.WithGenLocalState(func(ctx context.Context) *State {
        return &State{}
    }),
)
```

## 📚 学习资源

- **官方文档**: https://www.cloudwego.io/docs/eino/
- **GitHub**: https://github.com/cloudwego/eino
- **示例仓库**: https://github.com/cloudwego/eino-examples

## 🐛 常见问题

### Q: 为什么需要设置 API Key？
A: Eino 需要调用 LLM 服务，需要相应服务商的 API Key。你也可以使用本地模型（如 Ollama）。

### Q: 如何切换到其他模型？
A: 修改代码中的模型初始化部分，例如使用豆包：

```go
import "github.com/cloudwego/eino-ext/components/model/ark"

model, _ := ark.NewChatModel(ctx, &ark.ChatModelConfig{
    Model: "doubao-pro-32k",
})
```

### Q: Graph 和 Chain 有什么区别？
A: 
- **Chain**: 线性流程，节点按顺序执行
- **Graph**: 支持分支、循环等复杂流程控制

## 📝 下一步

1. ✅ 尝试运行所有示例
2. ✅ 修改示例代码，理解各个组件
3. ✅ 创建自己的工具
4. ✅ 构建一个简单的 RAG 应用
5. ✅ 探索 Multi-Agent 模式

Happy Coding with Eino! 🎉
