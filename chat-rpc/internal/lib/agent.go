package lib

import (
	"context"
	"fmt"
	"strings"

	einoopenai "github.com/cloudwego/eino-ext/components/model/openai"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/flow/agent/react"
	"github.com/cloudwego/eino/schema"

	"awesomeProject/chat-rpc/internal/tools"
)

// Registry 管理所有注册的 Agent
type Registry struct {
	agents map[string]*react.Agent
	cfg    *PromptConfig
}

// NewRegistry 初始化 Agent 注册表
func NewRegistry(ctx context.Context, cfg *PromptConfig, kimiKey, glmKey string) (*Registry, error) {
	r := &Registry{
		agents: make(map[string]*react.Agent),
		cfg:    cfg,
	}

	// 注册工具列表
	toolList := []tool.BaseTool{
		&tools.CalculatorTool{},
		&tools.TimeTool{},
		tools.NewStockTool(),
	}

	systemPrompt := BuildSystemPrompt(cfg)
	messageModifier := func(ctx context.Context, input []*schema.Message) []*schema.Message {
		msgs := make([]*schema.Message, 0, len(input)+1)
		msgs = append(msgs, schema.SystemMessage(systemPrompt))
		msgs = append(msgs, input...)
		return msgs
	}

	// 1. Kimi K2
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
	if glmKey != "" {
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
	}

	return r, nil
}

// Get 获取指定模型的 Agent
func (r *Registry) Get(modelID string) (*react.Agent, bool) {
	a, ok := r.agents[modelID]
	return a, ok
}

// MatchImage 根据消息内容匹配图片
func (r *Registry) MatchImage(userMsg, reply string) string {
	if r.cfg == nil {
		return ""
	}
	msgLower := strings.ToLower(userMsg)
	for _, rule := range r.cfg.Rules {
		triggerLower := strings.ToLower(rule.Trigger)
		if rule.Image != "" && (strings.Contains(msgLower, triggerLower) || strings.Contains(triggerLower, msgLower)) {
			return "/image/" + rule.Image
		}
	}
	return ""
}
