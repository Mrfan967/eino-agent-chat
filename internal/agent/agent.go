package agent

import (
	"context"
	"fmt"

	einoopenai "github.com/cloudwego/eino-ext/components/model/openai"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/flow/agent/react"
	"github.com/cloudwego/eino/schema"

	"awesomeProject/internal/config"
	"awesomeProject/internal/rag"
	"awesomeProject/internal/tools"
)

// Registry 管理所有注册的 Agent
type Registry struct {
	agents map[string]*react.Agent
}

// NewRegistry 初始化并返回 Agent 注册表
func NewRegistry(ctx context.Context, ragSvc *rag.Service, cfg *config.PromptConfig, apiKeys map[string]string) (*Registry, error) {
	r := &Registry{agents: make(map[string]*react.Agent)}

	toolList := []tool.BaseTool{
		&tools.CalculatorTool{},
		&tools.TimeTool{},
		&tools.KnowledgeTool{RAG: ragSvc},
	}

	systemPrompt := buildSystemPrompt(cfg)
	messageModifier := func(ctx context.Context, input []*schema.Message) []*schema.Message {
		msgs := make([]*schema.Message, 0, len(input)+1)
		msgs = append(msgs, schema.SystemMessage(systemPrompt))
		msgs = append(msgs, input...)
		return msgs
	}

	// 1. Kimi K2
	kimiKey := apiKeys["MOONSHOT_API_KEY"]
	if kimiKey == "" {
		kimiKey = "sk-5rKOvSmwV015EurXmJaSLSdsnk8tOEFdQkCJkLpfJrBiELIb"
	}
	kimiModel, err := einoopenai.NewChatModel(ctx, &einoopenai.ChatModelConfig{
		APIKey:  kimiKey,
		BaseURL: "https://api.moonshot.cn/v1",
		Model:   "moonshot-v1-8k",
	})
	if err != nil {
		return nil, fmt.Errorf("kimi model init failed: %v", err)
	}
	kimiAgent, err := react.NewAgent(ctx, &react.AgentConfig{
		ToolCallingModel: kimiModel,
		ToolsConfig:      compose.ToolsNodeConfig{Tools: toolList},
		MessageModifier:  messageModifier,
	})
	if err != nil {
		return nil, fmt.Errorf("kimi agent init failed: %v", err)
	}
	r.agents["kimi-k2"] = kimiAgent

	// 2. 智谱 GLM-4
	glmKey := apiKeys["ZHIPU_API_KEY"]
	if glmKey == "" {
		glmKey = apiKeys["OPENAI_API_KEY"]
	}
	if glmKey == "" {
		fmt.Println("⚠️  未设置 ZHIPU_API_KEY 或 OPENAI_API_KEY 环境变量，智谱 GLM 模型将无法调用")
	}
	glmModel, err := einoopenai.NewChatModel(ctx, &einoopenai.ChatModelConfig{
		APIKey:  glmKey,
		BaseURL: "https://open.bigmodel.cn/api/paas/v4",
		Model:   "glm-4.1v-thinking-flash",
	})
	if err == nil {
		glmAgent, err := react.NewAgent(ctx, &react.AgentConfig{
			ToolCallingModel: glmModel,
			ToolsConfig:      compose.ToolsNodeConfig{Tools: toolList},
			MessageModifier:  messageModifier,
		})
		if err == nil {
			r.agents["glm-4"] = glmAgent
		}
	}

	return r, nil
}

// Get 获取指定模型的 Agent
func (r *Registry) Get(modelID string) (*react.Agent, bool) {
	a, ok := r.agents[modelID]
	return a, ok
}

// buildSystemPrompt 根据配置构建系统提示词
func buildSystemPrompt(cfg *config.PromptConfig) string {
	if cfg == nil {
		return "你是智能助手"
	}

	prompt := cfg.Persona + "\n在回答问题时，请遵循以下规则：\n\n"
	for i, r := range cfg.Rules {
		prompt += fmt.Sprintf("条目%d. 当用户问\"%s\"或类似问题时，回答：%s\n\n", i+1, r.Trigger, r.Answer)
	}

	if len(cfg.SystemRules) > 0 {
		prompt += "系统全局规则：\n"
		for i, sr := range cfg.SystemRules {
			prompt += fmt.Sprintf("- 规则%d: %s\n", i+1, sr)
		}
		prompt += "\n"
	}

	if cfg.Fallback != "" {
		prompt += "兜底策略：" + cfg.Fallback
	}
	return prompt
}
