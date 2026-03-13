@echo off
chcp 65001 >nul
:: Agent 智能对话平台快速部署脚本 (Windows)

:: 1. 设置环境变量
:: 请在这里填入你的 API Key (如果留空，可能在网页端调用模型时会报错)
set ZHIPU_API_KEY=5f1faa9546f04e1fa2f20a231cc2deaa.83s9Pt9iAbGN6TO7
set MOONSHOT_API_KEY=sk-5rKOvSmwV015EurXmJaSLSdsnk8tOEFdQkCJkLpfJrBiELIb

echo ----------------------------------------
echo 🚀 开始编译 Agent 智能对话平台...
echo ----------------------------------------

:: 2. 编译项目
go build -o awesomeProject.exe .

:: 3. 检查编译结果并运行
if %ERRORLEVEL% equ 0 (
    echo ✅ 编译成功！正在启动服务...
    echo ----------------------------------------
    .\awesomeProject.exe
) else (
    echo ❌ 编译失败，请检查代码会有语法错误！
)

pause
