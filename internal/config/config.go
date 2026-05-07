package config

import (
	"encoding/json"
	"os"
)

// PromptConfig 提示词配置
type PromptConfig struct {
	Persona     string   `json:"persona"`
	SystemRules []string `json:"system_rules"`
	Rules       []Rule   `json:"rules"`
	Fallback    string   `json:"fallback"`
}

// Rule 单条触发规则
type Rule struct {
	Trigger string `json:"trigger"`
	Answer  string `json:"answer"`
	Image   string `json:"image"`
}

// Load 读取 prompt_config.json
func Load(path string) (*PromptConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cfg PromptConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// Default 返回默认配置
func Default() *PromptConfig {
	return &PromptConfig{
		Persona: "你是人工智能助手，擅长中文和英文的对话。",
	}
}
