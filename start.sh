#!/bin/bash
# Agent 智能对话平台快速部署脚本 (Linux)

# 1. 设置环境变量 (使用你提供的最新 Key)
export ZHIPU_API_KEY="5f1faa9546f04e1fa2f20a231cc2deaa.83s9Pt9iAbGN6TO7"
export MOONSHOT_API_KEY="sk-5rKOvSmwV015EurXmJaSLSdsnk8tOEFdQkCJkLpfJrBiELIb"

echo "----------------------------------------"
echo "🚀 开始在服务器编译项目..."
echo "----------------------------------------"

# 2. 编译项目
go build -o awesomeProject_linux .

# 3. 检查编译结果并运行
if [ $? -eq 0 ]; then
    echo "✅ 编译成功！正在启动服务..."
    echo "----------------------------------------"
    chmod +x awesomeProject_linux
    ./awesomeProject_linux
else
    echo "❌ 编译失败，请检查服务器 Go 环境或代码错误！"
fi
