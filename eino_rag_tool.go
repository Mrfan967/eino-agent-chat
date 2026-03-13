package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

// KnowledgeTool 知识库检索工具
type KnowledgeTool struct{}

func (k *KnowledgeTool) Info(ctx context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{
		Name: "knowledge_retriever",
		Desc: "从内部知识库和系统文档中检索相关信息。当用户询问特定人物事迹、内部规定、系统信息等知识时，必须首先调用此工具获取资料。",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"query": {
				Type:     schema.String,
				Desc:     "要检索的查询语句，请提取用户问题中的核心关键词",
				Required: true,
			},
		}),
	}, nil
}

func (k *KnowledgeTool) InvokableRun(ctx context.Context, argumentsInJSON string, opts ...tool.Option) (string, error) {
	if GlobalRAGStore == nil {
		return "系统未初始化知识库", nil
	}

	var params map[string]interface{}
	if err := json.Unmarshal([]byte(argumentsInJSON), &params); err != nil {
		return "", fmt.Errorf("解析参数失败: %v", err)
	}

	query, ok := params["query"].(string)
	if !ok || query == "" {
		return "请输入有效的查询参数 query", nil
	}

	// 检索 Top-3 相关文档
	results, err := GlobalRAGStore.Search(ctx, query, 3)
	if err != nil {
		return "", fmt.Errorf("检索知识库失败: %v", err)
	}

	if len(results) == 0 {
		return "在知识库中未找到相关内容", nil
	}

	// 组合结果
	var sb strings.Builder
	sb.WriteString("从知识库中检索到以下相关信息：\n\n")
	for i, res := range results {
		sb.WriteString(fmt.Sprintf("---\n片段 %d:\n%s\n", i+1, res))
	}

	return sb.String(), nil
}
