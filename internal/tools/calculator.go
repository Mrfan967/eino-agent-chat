package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

// CalculatorTool 计算工具
type CalculatorTool struct{}

func (c *CalculatorTool) Info(ctx context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{
		Name: "calculator",
		Desc: "执行基本的数学运算，支持加减乘除",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"operation": {
				Type:     schema.String,
				Desc:     "运算符：add, subtract, multiply, divide",
				Required: true,
			},
			"a": {
				Type:     schema.Number,
				Desc:     "第一个数字",
				Required: true,
			},
			"b": {
				Type:     schema.Number,
				Desc:     "第二个数字",
				Required: true,
			},
		}),
	}, nil
}

func (c *CalculatorTool) InvokableRun(ctx context.Context, argumentsInJSON string, opts ...tool.Option) (string, error) {
	var params map[string]interface{}
	if err := json.Unmarshal([]byte(argumentsInJSON), &params); err != nil {
		return "", fmt.Errorf("解析参数失败: %v", err)
	}

	operation := params["operation"].(string)
	a := params["a"].(float64)
	b := params["b"].(float64)

	var result float64
	switch operation {
	case "add":
		result = a + b
	case "subtract":
		result = a - b
	case "multiply":
		result = a * b
	case "divide":
		if b == 0 {
			return "", fmt.Errorf("除数不能为0")
		}
		result = a / b
	default:
		return "", fmt.Errorf("不支持的运算符: %s", operation)
	}

	return fmt.Sprintf("%.2f", result), nil
}
