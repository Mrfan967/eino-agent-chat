package lib

import (
	"encoding/json"
	"fmt"
	"os"
)

// PromptConfig 提示词配置
type PromptConfig struct {
	Persona     string   `json:"persona"`
	SystemRules []string `json:"system_rules"`
	Rules       []Rule   `json:"rules"`
	Fallback    string   `json:"fallback"`
}

// Rule 触发规则
type Rule struct {
	Trigger string `json:"trigger"`
	Answer  string `json:"answer"`
	Image   string `json:"image"`
}

// LoadPromptConfig 从文件加载提示词配置
func LoadPromptConfig(path string) (*PromptConfig, error) {
	if path == "" {
		path = "../prompt_config.json"
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("读取提示词配置文件失败: %v", err)
	}
	var cfg PromptConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("解析提示词配置失败: %v", err)
	}
	return &cfg, nil
}

// BuildSystemPrompt 根据配置构建系统提示词
func BuildSystemPrompt(cfg *PromptConfig) string {
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
