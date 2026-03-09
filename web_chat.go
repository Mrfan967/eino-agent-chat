package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/flow/agent/react"
	"github.com/cloudwego/eino/schema"
	"github.com/cloudwego/eino-ext/components/model/openai"
)

// chatRequest 请求结构
type chatRequest struct {
	Message string `json:"message"`
	Model   string `json:"model"`
}

// chatResponse 响应结构
type chatResponse struct {
	Reply string `json:"reply"`
	Image string `json:"image,omitempty"`
	Error string `json:"error,omitempty"`
}

// StartWebChat 启动 Web 聊天服务
func StartWebChat() {
	ctx := context.Background()

	// 建立一个多模型专属的 Agent 缓存池，以及初始化需要的基础资源
	agentCache := make(map[string]*react.Agent)
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		fmt.Println("❌ OPENAI_API_KEY 未设置，无法启动 Web 对话")
		return
	}
	tools := []tool.BaseTool{&CalculatorTool{}, &TimeTool{}}

	// 从配置文件读取 System Prompt
	type rule struct {
		Trigger string `json:"trigger"`
		Answer  string `json:"answer"`
		Image   string `json:"image"`
	}
	type promptConfig struct {
		Persona  string `json:"persona"`
		Rules    []rule `json:"rules"`
		Fallback string `json:"fallback"`
	}
	promptData, err := os.ReadFile("prompt_config.json")
	if err != nil {
		fmt.Printf("⚠️  读取 prompt_config.json 失败: %v，将使用默认提示\n", err)
		promptData = []byte(`{"persona":"你是一个智能助手，请友好地回答用户的问题。","rules":[],"fallback":""}`)
	}
	var cfg promptConfig
	if err := json.Unmarshal(promptData, &cfg); err != nil {
		fmt.Printf("⚠️  解析 prompt_config.json 失败: %v\n", err)
		return
	}

	// 拼接 System Prompt
	systemPrompt := cfg.Persona + "\n在回答问题时，请遵循以下规则：\n\n"
	for i, r := range cfg.Rules {
		systemPrompt += fmt.Sprintf("%d. 当用户问\"%s\"或类似问题时，回答：%s\n\n", i+1, r.Trigger, r.Answer)
	}
	if cfg.Fallback != "" {
		systemPrompt += cfg.Fallback
	}
	fmt.Printf("✅ 已加载 System Prompt（%d 条规则）\n", len(cfg.Rules))

	// 提供一个获取对应模型 Agent 的工厂函数
	getOrCreateAgent := func(ctx context.Context, modelName string) (*react.Agent, error) {
		if modelName == "" {
			modelName = "glm-4.1v-thinking-flash" // 默认模型
		}
		
		if agent, ok := agentCache[modelName]; ok {
			return agent, nil
		}

		chatModel, err := openai.NewChatModel(ctx, &openai.ChatModelConfig{
			APIKey:  apiKey,
			BaseURL: "https://open.bigmodel.cn/api/paas/v4",
			Model:   modelName,
		})
		if err != nil {
			return nil, fmt.Errorf("创建 ChatModel 失败: %v", err)
		}

		agent, err := react.NewAgent(ctx, &react.AgentConfig{
			ToolCallingModel: chatModel,
			ToolsConfig: compose.ToolsNodeConfig{Tools: tools},
			MessageModifier: func(ctx context.Context, input []*schema.Message) []*schema.Message {
				msgs := make([]*schema.Message, 0, len(input)+1)
				msgs = append(msgs, schema.SystemMessage(systemPrompt))
				msgs = append(msgs, input...)
				return msgs
			},
		})
		if err != nil {
			return nil, fmt.Errorf("创建 Agent 失败: %v", err)
		}
		
		agentCache[modelName] = agent
		return agent, nil
	}

	// 聊天 API
	http.HandleFunc("/api/chat", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
			return
		}

		var req chatRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			json.NewEncoder(w).Encode(chatResponse{Error: "请求格式错误"})
			return
		}

		if req.Message == "" {
			json.NewEncoder(w).Encode(chatResponse{Error: "消息不能为空"})
			return
		}

		messages := []*schema.Message{
			schema.UserMessage(req.Message),
		}

		agent, err := getOrCreateAgent(ctx, req.Model)
		if err != nil {
			json.NewEncoder(w).Encode(chatResponse{Error: err.Error()})
			return
		}

		result, err := agent.Generate(ctx, messages)

		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		if err != nil {
			json.NewEncoder(w).Encode(chatResponse{Error: fmt.Sprintf("Agent 出错: %v", err)})
			return
		}

		// 匹配图片：1. 输入精确匹配 Trigger
		var imageURL string
		msgLower := strings.ToLower(req.Message)
		for _, r := range cfg.Rules {
			triggerLower := strings.ToLower(r.Trigger)
			if r.Image != "" && (strings.Contains(msgLower, triggerLower) || strings.Contains(triggerLower, msgLower)) {
				imageURL = "/image/" + r.Image
				break
			}
		}

		// 2. 智能兜底判定：如果规则没命中，但 AI 回答里提到了具体的人（由 Fallback 机制生成的回答）
		if imageURL == "" {
			replyLower := strings.ToLower(result.Content)
			if strings.Contains(replyLower, "赵苏通") {
				imageURL = "/image/su.jpg"
			} else if strings.Contains(replyLower, "范晨旭") {
				imageURL = "/image/fan.jpg"
			}
		}

		json.NewEncoder(w).Encode(chatResponse{Reply: result.Content, Image: imageURL})
	})

	// 图片静态文件服务
	os.MkdirAll("image", 0755)
	http.Handle("/image/", http.StripPrefix("/image/", http.FileServer(http.Dir("image"))))

	// 页面
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(chatHTML))
	})

	addr := ":8080"
	fmt.Println("\n🌐 Web 对话服务已启动！")
	fmt.Println("   浏览器访问: http://localhost" + addr)
	fmt.Println("   按 Ctrl+C 停止服务\n")

	// 自动打开浏览器
	openBrowser("http://localhost" + addr)

	if err := http.ListenAndServe(addr, nil); err != nil {
		fmt.Printf("服务启动失败: %v\n", err)
	}
}

func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	case "darwin":
		cmd = exec.Command("open", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	cmd.Start()
}

// 内嵌的聊天页面 HTML
const chatHTML = `<!DOCTYPE html>
<html lang="zh-CN">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>Eino Agent 对话</title>
<link href="https://fonts.googleapis.com/css2?family=Inter:wght@400;500;600&display=swap" rel="stylesheet">
<style>
  * { margin: 0; padding: 0; box-sizing: border-box; }

  body {
    font-family: 'Inter', -apple-system, BlinkMacSystemFont, sans-serif;
    background: #0f0f14;
    color: #e4e4e7;
    height: 100vh;
    display: flex;
    flex-direction: column;
  }

  /* 顶栏 */
  .header {
    background: linear-gradient(135deg, #1a1a2e 0%, #16162a 100%);
    border-bottom: 1px solid rgba(139, 92, 246, 0.2);
    padding: 16px 24px;
    display: flex;
    align-items: center;
    gap: 12px;
    flex-shrink: 0;
  }

  .header .logo {
    width: 40px; height: 40px;
    background: linear-gradient(135deg, #8b5cf6, #6d28d9);
    border-radius: 12px;
    display: flex;
    align-items: center;
    justify-content: center;
    font-size: 20px;
    box-shadow: 0 4px 15px rgba(139, 92, 246, 0.3);
  }

  .header h1 {
    margin: 0;
    font-size: 18px;
    font-weight: 600;
    color: #f4f4f5;
  }
  .header .subtitle {
    font-size: 12px;
    color: #a1a1aa;
    margin-top: 2px;
  }
  .model-selector {
    margin-right: 15px;
  }
  .model-select {
    padding: 6px 30px 6px 12px;
    font-size: 13px;
    color: #e4e4e7;
    background-color: #27272a;
    border: 1px solid #3f3f46;
    border-radius: 6px;
    appearance: none;
    cursor: pointer;
    background-image: url("data:image/svg+xml;charset=US-ASCII,%3Csvg%20xmlns%3D%22http%3A%2F%2Fwww.w3.org%2F2000%2Fsvg%22%20width%3D%22292.4%22%20height%3D%22292.4%22%3E%3Cpath%20fill%3D%22%23a1a1aa%22%20d%3D%22M287%2069.4a17.6%2017.6%200%200%200-13-5.4H18.4c-5%200-9.3%201.8-12.9%205.4A17.6%2017.6%200%200%200%200%2082.2c0%205%201.8%209.3%205.4%2012.9l128%20127.9c3.6%203.6%207.8%205.4%2012.8%205.4s9.2-1.8%2012.8-5.4L287%2095c3.5-3.5%205.4-7.8%205.4-12.8%200-5-1.9-9.2-5.5-12.8z%22%2F%3E%3C%2Fsvg%3E");
    background-repeat: no-repeat;
    background-position: right 10px top 50%;
    background-size: 10px auto;
    transition: all 0.2s;
  }
  .model-select:hover {
    border-color: #52525b;
  }
  .model-select:focus {
    outline: none;
    border-color: #3b82f6;
    box-shadow: 0 0 0 2px rgba(59, 130, 246, 0.2);
  }
  .model-select option {
    background-color: #18181b;
    color: #e4e4e7;
  }
  .status {
    margin-left: auto;
    display: flex;
    align-items: center;
    gap: 6px;
    font-size: 12px;
    color: #a1a1aa;
  }

  .header .status-dot {
    width: 8px; height: 8px;
    background: #22c55e;
    border-radius: 50%;
    animation: pulse 2s ease-in-out infinite;
  }

  @keyframes pulse {
    0%, 100% { opacity: 1; }
    50% { opacity: 0.5; }
  }

  /* 功能标签 */
  .capabilities {
    display: flex;
    justify-content: center;
    gap: 8px;
    padding: 12px 24px;
    background: rgba(139, 92, 246, 0.05);
    border-bottom: 1px solid rgba(255,255,255,0.04);
    flex-shrink: 0;
  }

  .cap-tag {
    padding: 4px 12px;
    border-radius: 20px;
    font-size: 11px;
    background: rgba(139, 92, 246, 0.1);
    color: #a78bfa;
    border: 1px solid rgba(139, 92, 246, 0.2);
  }

  /* 消息区 */
  .messages {
    flex: 1;
    overflow-y: auto;
    padding: 24px;
    display: flex;
    flex-direction: column;
    gap: 16px;
    scroll-behavior: smooth;
  }

  .messages::-webkit-scrollbar { width: 4px; }
  .messages::-webkit-scrollbar-thumb {
    background: rgba(139, 92, 246, 0.3);
    border-radius: 2px;
  }

  .msg {
    display: flex;
    gap: 12px;
    max-width: 80%;
    animation: fadeIn 0.3s ease;
  }

  @keyframes fadeIn {
    from { opacity: 0; transform: translateY(8px); }
    to { opacity: 1; transform: translateY(0); }
  }

  .msg.user { align-self: flex-end; flex-direction: row-reverse; }
  .msg.agent { align-self: flex-start; }

  .msg .avatar {
    width: 36px; height: 36px;
    border-radius: 10px;
    display: flex;
    align-items: center;
    justify-content: center;
    font-size: 16px;
    flex-shrink: 0;
  }

  .msg.user .avatar {
    background: linear-gradient(135deg, #3b82f6, #1d4ed8);
  }

  .msg.agent .avatar {
    background: linear-gradient(135deg, #8b5cf6, #6d28d9);
  }

  .msg .bubble {
    padding: 12px 16px;
    border-radius: 16px;
    line-height: 1.6;
    font-size: 14px;
    white-space: pre-wrap;
    word-break: break-word;
  }

  .msg.user .bubble {
    background: linear-gradient(135deg, #3b82f6, #2563eb);
    color: #fff;
    border-bottom-right-radius: 4px;
  }

  .msg.agent .bubble {
    background: #1e1e2e;
    color: #e4e4e7;
    border: 1px solid rgba(255,255,255,0.06);
    border-bottom-left-radius: 4px;
  }

  .msg.error .bubble {
    background: rgba(239, 68, 68, 0.1);
    border: 1px solid rgba(239, 68, 68, 0.3);
    color: #fca5a5;
  }

  .chat-img {
    display: block;
    max-width: 280px;
    border-radius: 12px;
    margin-top: 10px;
    cursor: pointer;
    transition: transform 0.2s;
  }

  .chat-img:hover {
    transform: scale(1.03);
  }

  /* 加载动画 */
  .typing {
    display: flex;
    gap: 4px;
    padding: 16px 20px;
  }

  .typing span {
    width: 8px; height: 8px;
    background: #8b5cf6;
    border-radius: 50%;
    animation: bounce 1.4s infinite ease-in-out;
  }

  .typing span:nth-child(2) { animation-delay: 0.16s; }
  .typing span:nth-child(3) { animation-delay: 0.32s; }

  @keyframes bounce {
    0%, 80%, 100% { transform: scale(0.6); opacity: 0.4; }
    40% { transform: scale(1); opacity: 1; }
  }

  /* 欢迎消息 */
  .welcome {
    text-align: center;
    padding: 48px 24px;
    animation: fadeIn 0.5s ease;
  }

  .welcome .icon { font-size: 48px; margin-bottom: 16px; }

  .welcome h2 {
    font-size: 22px;
    font-weight: 600;
    margin-bottom: 8px;
    background: linear-gradient(135deg, #c4b5fd, #8b5cf6);
    -webkit-background-clip: text;
    -webkit-text-fill-color: transparent;
  }

  .welcome p {
    color: #71717a;
    font-size: 14px;
    margin-bottom: 24px;
  }

  .suggestions {
    display: flex;
    flex-wrap: wrap;
    justify-content: center;
    gap: 8px;
  }

  .suggestion {
    padding: 8px 16px;
    border-radius: 20px;
    font-size: 13px;
    background: rgba(139, 92, 246, 0.08);
    color: #a78bfa;
    border: 1px solid rgba(139, 92, 246, 0.2);
    cursor: pointer;
    transition: all 0.2s;
  }

  .suggestion:hover {
    background: rgba(139, 92, 246, 0.15);
    border-color: rgba(139, 92, 246, 0.4);
    transform: translateY(-1px);
  }

  /* 输入区 */
  .input-area {
    padding: 16px 24px 24px;
    background: linear-gradient(180deg, transparent, rgba(15, 15, 20, 0.8));
    flex-shrink: 0;
  }

  .input-box {
    display: flex;
    align-items: center;
    gap: 12px;
    background: #1e1e2e;
    border: 1px solid rgba(139, 92, 246, 0.2);
    border-radius: 16px;
    padding: 4px 4px 4px 20px;
    transition: border-color 0.2s, box-shadow 0.2s;
  }

  .input-box:focus-within {
    border-color: rgba(139, 92, 246, 0.5);
    box-shadow: 0 0 20px rgba(139, 92, 246, 0.1);
  }

  .input-box input {
    flex: 1;
    background: none;
    border: none;
    outline: none;
    color: #e4e4e7;
    font-size: 14px;
    font-family: inherit;
    padding: 12px 0;
  }

  .input-box input::placeholder { color: #52525b; }

  .input-box button {
    width: 44px; height: 44px;
    border-radius: 12px;
    border: none;
    background: linear-gradient(135deg, #8b5cf6, #6d28d9);
    color: white;
    font-size: 18px;
    cursor: pointer;
    display: flex;
    align-items: center;
    justify-content: center;
    transition: all 0.2s;
    flex-shrink: 0;
  }

  .input-box button:hover {
    transform: scale(1.05);
    box-shadow: 0 4px 15px rgba(139, 92, 246, 0.4);
  }

  .input-box button:disabled {
    opacity: 0.4;
    cursor: not-allowed;
    transform: none;
    box-shadow: none;
  }

  .footer-hint {
    text-align: center;
    font-size: 11px;
    color: #3f3f46;
    margin-top: 8px;
  }
</style>
</head>
<body>

<div class="header">
  <div class="logo">🤖</div>
  <div>
    <h1>Eino Agent</h1>
    <div class="subtitle">全能智能助手</div>
  </div>
  <div class="model-selector">
    <select id="modelSelect" class="model-select">
      <option value="glm-4.7-flash" title="当前主推的免费主力模型">GLM-4.7-Flash (免费主力)</option>
      <option value="glm-4.1v-thinking-flash" selected title="带思考过程的模型">GLM-4.1V-Thinking-Flash (思考模式)</option>
      <option value="glm-4-flash" title="最基础的响应速度最快">GLM-4-Flash (极速基础)</option>
    </select>
  </div>
  <div class="status">
    <div class="status-dot"></div>
    在线
  </div>
</div>

<div class="capabilities">
  <span class="cap-tag">💬 智能对话</span>
  <span class="cap-tag">🧮 数学计算</span>
  <span class="cap-tag">🕐 时间查询</span>
</div>

<div class="messages" id="messages">
  <div class="welcome" id="welcome">
    <div class="icon">✨</div>
    <h2>你好，有什么可以帮你的？</h2>
    <p>我是 Eino Agent，可以和你聊天、帮你计算、查询时间</p>
    <div class="suggestions">
      <div class="suggestion" onclick="sendSuggestion(this)">现在几点了？</div>
      <div class="suggestion" onclick="sendSuggestion(this)">帮我算 1024 × 768</div>
      <div class="suggestion" onclick="sendSuggestion(this)">介绍一下 Go 语言</div>
      <div class="suggestion" onclick="sendSuggestion(this)">什么是 Goroutine？</div>
    </div>
  </div>
</div>

<div class="input-area">
  <div class="input-box">
    <input type="text" id="input" placeholder="输入消息..." autocomplete="off" />
    <button id="sendBtn" onclick="sendMessage()">➤</button>
  </div>
  <div class="footer-hint">按 Enter 发送消息</div>
</div>

<script>
const messagesEl = document.getElementById('messages');
const inputEl = document.getElementById('input');
const sendBtn = document.getElementById('sendBtn');
const welcomeEl = document.getElementById('welcome');
let isLoading = false;

inputEl.addEventListener('keydown', e => {
  if (e.key === 'Enter' && !e.shiftKey && !isLoading) {
    e.preventDefault();
    sendMessage();
  }
});

function sendSuggestion(el) {
  inputEl.value = el.textContent;
  sendMessage();
}

function addMessage(text, role, imageUrl) {
  if (welcomeEl) welcomeEl.remove();

  const div = document.createElement('div');
  div.className = 'msg ' + role;

  const avatar = document.createElement('div');
  avatar.className = 'avatar';
  avatar.textContent = role === 'user' ? '👤' : '🤖';

  const bubble = document.createElement('div');
  bubble.className = 'bubble';
  bubble.textContent = text;

  if (imageUrl) {
    const img = document.createElement('img');
    img.src = imageUrl;
    img.className = 'chat-img';
    img.onclick = () => window.open(imageUrl, '_blank');
    bubble.appendChild(img);
  }

  div.appendChild(avatar);
  div.appendChild(bubble);
  messagesEl.appendChild(div);
  messagesEl.scrollTop = messagesEl.scrollHeight;
  return div;
}

function showTyping() {
  const div = document.createElement('div');
  div.className = 'msg agent';
  div.id = 'typing';

  const avatar = document.createElement('div');
  avatar.className = 'avatar';
  avatar.textContent = '🤖';

  const bubble = document.createElement('div');
  bubble.className = 'bubble';
  bubble.innerHTML = '<div class="typing"><span></span><span></span><span></span></div>';

  div.appendChild(avatar);
  div.appendChild(bubble);
  messagesEl.appendChild(div);
  messagesEl.scrollTop = messagesEl.scrollHeight;
}

function removeTyping() {
  const el = document.getElementById('typing');
  if (el) el.remove();
}

async function sendMessage() {
  const text = inputEl.value.trim();
  if (!text || isLoading) return;

  isLoading = true;
  sendBtn.disabled = true;
  inputEl.value = '';

  addMessage(text, 'user');
  showTyping();

  try {
    const selectedModel = document.getElementById('modelSelect').value;
    const resp = await fetch('/api/chat', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ message: text, model: selectedModel })
    });
    const data = await resp.json();
    removeTyping();

    if (data.error) {
      addMessage('❌ ' + data.error, 'error');
    } else {
      addMessage(data.reply, 'agent', data.image);
    }
  } catch (err) {
    removeTyping();
    addMessage('❌ 网络错误，请重试', 'error');
  }

  isLoading = false;
  sendBtn.disabled = false;
  inputEl.focus();
}

inputEl.focus();
</script>
</body>
</html>`
