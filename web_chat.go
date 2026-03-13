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
	"sync"

	"github.com/cloudwego/eino-ext/components/model/openai"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/flow/agent/react"
	"github.com/cloudwego/eino/schema"
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

// streamChunk 流式输出的单个数据块
type streamChunk struct {
	Content string `json:"content,omitempty"`
	Error   string `json:"error,omitempty"`
	Done    bool   `json:"done,omitempty"`
	Image   string `json:"image,omitempty"`
}

// promptConfig 提示词配置
type promptConfig struct {
	Persona  string `json:"persona"`
	Rules    []rule `json:"rules"`
	Fallback string `json:"fallback"`
}

type rule struct {
	Trigger string `json:"trigger"`
	Answer  string `json:"answer"`
	Image   string `json:"image"`
}

// Global state
var (
	agents              = make(map[string]*react.Agent)
	conversationHistory = make([]*schema.Message, 0)
	historyMutex        sync.Mutex
)

// StartWebChat 启动 Web 聊天服务
func StartWebChat() {
	ctx := context.Background()

	// 1. 初始化模型和 Agent
	err := initAgents(ctx)
	if err != nil {
		fmt.Printf("初始化 Agents 失败: %v\n", err)
		return
	}

	promptData, err := readPromptConfig()
	if err != nil {
		fmt.Printf("⚠️  读取 prompt_config.json 失败: %v，将使用默认提示\n", err)
		promptData = &promptConfig{
			Persona: "你是人工智能助手，擅长中文和英文的对话。",
		}
	}
	cfg := promptData

	// ======================== SSE 流式聊天 API ========================
	http.HandleFunc("/api/chat/stream", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
			return
		}

		var req chatRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "请求格式错误", http.StatusBadRequest)
			return
		}
		if req.Message == "" {
			http.Error(w, "消息不能为空", http.StatusBadRequest)
			return
		}

		modelID := req.Model
		if modelID == "" {
			modelID = "kimi-k2" // 默认模型
		}

		agent, ok := agents[modelID]
		if !ok {
			http.Error(w, "不支持的模型", http.StatusBadRequest)
			return
		}

		// 设置 SSE 响应头
		w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("X-Accel-Buffering", "no")

		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "不支持流式输出", http.StatusInternalServerError)
			return
		}

		// 追加用户消息到历史
		userMsg := schema.UserMessage(req.Message)

		historyMutex.Lock()
		conversationHistory = append(conversationHistory, userMsg)
		// 拷贝历史用于当前请求
		messages := make([]*schema.Message, len(conversationHistory))
		copy(messages, conversationHistory)
		historyMutex.Unlock()

		// 使用 Agent 的 Stream 方法。注意 React Agent 的 Stream / Generate 接收的是 []*schema.Message
		streamResult, err := agent.Stream(r.Context(), messages)
		if err != nil {
			chunk, _ := json.Marshal(streamChunk{Error: fmt.Sprintf("Agent 出错: %v", err)})
			fmt.Fprintf(w, "data: %s\n\n", chunk)
			flusher.Flush()
			fmt.Fprintf(w, "data: [DONE]\n\n")
			flusher.Flush()

			// 失败时从历史中移除刚刚添加的用户消息
			historyMutex.Lock()
			if len(conversationHistory) > 0 {
				conversationHistory = conversationHistory[:len(conversationHistory)-1]
			}
			historyMutex.Unlock()
			return
		}

		var fullContent strings.Builder

		// 逐 chunk 读取并推送
		for {
			msg, err := streamResult.Recv()
			if err != nil {
				// 流结束
				break
			}
			if msg == nil {
				continue
			}

			content := msg.Content
			if content == "" {
				continue
			}

			fullContent.WriteString(content)

			chunk, _ := json.Marshal(streamChunk{Content: content})
			fmt.Fprintf(w, "data: %s\n\n", chunk)
			flusher.Flush()
		}

		finalReply := fullContent.String()

		// 追加 AI 回复到历史
		if finalReply != "" {
			historyMutex.Lock()
			conversationHistory = append(conversationHistory, schema.AssistantMessage(finalReply, nil))
			historyMutex.Unlock()
		}

		// 匹配图片
		imageURL := matchImage(cfg, req.Message, finalReply)
		if imageURL != "" {
			chunk, _ := json.Marshal(streamChunk{Image: imageURL})
			fmt.Fprintf(w, "data: %s\n\n", chunk)
			flusher.Flush()
		}

		// 发送结束标识
		fmt.Fprintf(w, "data: [DONE]\n\n")
		flusher.Flush()
	})

	// ======================== 清空历史 API ========================
	http.HandleFunc("/api/chat/clear", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
			return
		}
		historyMutex.Lock()
		conversationHistory = make([]*schema.Message, 0)
		historyMutex.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status":"ok"}`))
	})

	// 图片静态文件服务
	http.Handle("/image/", http.StripPrefix("/image/", http.FileServer(http.Dir("image"))))

	// 页面
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(chatHTML))
	})

	addr := ":8080"
	fmt.Println("\n🌐 Web 对话服务已启动！(支持多模型 + 上下文记忆)")
	fmt.Println("   浏览器访问: http://localhost" + addr)
	fmt.Println("   按 Ctrl+C 停止服务\n")

	openBrowser("http://localhost" + addr)

	if err := http.ListenAndServe(addr, nil); err != nil {
		fmt.Printf("服务启动失败: %v\n", err)
	}
}

func initAgents(ctx context.Context) error {
	tools := []tool.BaseTool{
		&CalculatorTool{},
		&TimeTool{},
		&KnowledgeTool{}, // 新增 RAG 工具
	}

	// 0. 初始化 RAG (优先使用智谱 Key，如果没有则回退 OpenAI Key)
	glmKey := os.Getenv("ZHIPU_API_KEY")
	if glmKey == "" {
		glmKey = os.Getenv("OPENAI_API_KEY")
	}
	if err := InitRAG(ctx, glmKey); err != nil {
		fmt.Printf("⚠️  RAG 知识库初始化失败: %v\n", err)
	} else {
		fmt.Printf("✅ RAG 知识库初始化成功 (如果 knowledge 目录下有文件)\n")
	}

	promptData, _ := readPromptConfig()
	systemPrompt := ""
	if promptData != nil {
		systemPrompt = promptData.Persona + "\n在回答问题时，请遵循以下规则：\n\n"
		for i, r := range promptData.Rules {
			systemPrompt += fmt.Sprintf("%d. 当用户问\"%s\"或类似问题时，回答：%s\n\n", i+1, r.Trigger, r.Answer)
		}
		if promptData.Fallback != "" {
			systemPrompt += promptData.Fallback
		}
	} else {
		systemPrompt = "你是智能助手"
	}

	// 追加知识检索的通用提示
	systemPrompt += "\n[系统提示: 当用户的问题涉及内部规定、人物事迹、技术细节等，请优先使用 knowledge_retriever 工具搜索最新资料。不要完全凭猜测回答。]\n"

	messageModifier := func(ctx context.Context, input []*schema.Message) []*schema.Message {
		msgs := make([]*schema.Message, 0, len(input)+1)
		msgs = append(msgs, schema.SystemMessage(systemPrompt))
		msgs = append(msgs, input...)
		return msgs
	}

	// 1. Kimi K2
	kimiKey := os.Getenv("MOONSHOT_API_KEY")
	if kimiKey == "" {
		kimiKey = "sk-5rKOvSmwV015EurXmJaSLSdsnk8tOEFdQkCJkLpfJrBiELIb" // 默认演示 Key
	}
	kimiModel, err := openai.NewChatModel(ctx, &openai.ChatModelConfig{
		APIKey:  kimiKey,
		BaseURL: "https://api.moonshot.cn/v1",
		Model:   "moonshot-v1-8k",
	})
	if err != nil {
		return fmt.Errorf("kimi model init failed: %v", err)
	}
	kimiAgent, err := react.NewAgent(ctx, &react.AgentConfig{
		ToolCallingModel: kimiModel,
		ToolsConfig:      compose.ToolsNodeConfig{Tools: tools},
		MessageModifier:  messageModifier,
	})
	if err != nil {
		return fmt.Errorf("kimi agent init failed: %v", err)
	}
	agents["kimi-k2"] = kimiAgent

	// 2. 智谱 GLM-4  (无条件注册，如果Key为空将在调用时报错而不是提示不支持该模型)
	glmKey = os.Getenv("ZHIPU_API_KEY")
	if glmKey == "" {
		glmKey = os.Getenv("OPENAI_API_KEY") // 兼容旧的环境变量
	}
	if glmKey == "" {
		fmt.Println("⚠️  未设置 ZHIPU_API_KEY 或 OPENAI_API_KEY 环境变量，智谱 GLM 模型将无法调用")
	}
	glmModel, err := openai.NewChatModel(ctx, &openai.ChatModelConfig{
		APIKey:  glmKey,
		BaseURL: "https://open.bigmodel.cn/api/paas/v4",
		Model:   "glm-4.1v-thinking-flash",
	})
	if err == nil {
		glmAgent, err := react.NewAgent(ctx, &react.AgentConfig{
			ToolCallingModel: glmModel,
			ToolsConfig:      compose.ToolsNodeConfig{Tools: tools},
			MessageModifier:  messageModifier,
		})
		if err == nil {
			agents["glm-4"] = glmAgent
		}
	}

	return nil
}

// readPromptConfig 读取提示词配置
func readPromptConfig() (*promptConfig, error) {
	data, err := os.ReadFile("prompt_config.json")
	if err != nil {
		return nil, err
	}
	var cfg promptConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// matchImage 匹配图片
func matchImage(cfg *promptConfig, userMsg, reply string) string {
	msgLower := strings.ToLower(userMsg)
	for _, r := range cfg.Rules {
		triggerLower := strings.ToLower(r.Trigger)
		if r.Image != "" && (strings.Contains(msgLower, triggerLower) || strings.Contains(triggerLower, msgLower)) {
			return "/image/" + r.Image
		}
	}

	// 智能兜底
	replyLower := strings.ToLower(reply)
	if strings.Contains(replyLower, "赵苏通") {
		return "/image/su.jpg"
	} else if strings.Contains(replyLower, "范晨旭") {
		return "/image/fan.jpg"
	}
	return ""
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
<title>Agent 智能对话平台</title>
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
    font-size: 18px;
    font-weight: 600;
    background: linear-gradient(135deg, #c4b5fd, #8b5cf6);
    -webkit-background-clip: text;
    -webkit-text-fill-color: transparent;
  }

  .header .controls {
    margin-left: auto;
    display: flex;
    align-items: center;
    gap: 12px;
  }

  select {
    background: #1e1e2e;
    color: #e4e4e7;
    border: 1px solid rgba(139, 92, 246, 0.4);
    border-radius: 8px;
    padding: 6px 12px;
    font-size: 13px;
    outline: none;
    cursor: pointer;
  }
  
  .clear-btn {
    background: rgba(239, 68, 68, 0.1);
    color: #fca5a5;
    border: 1px solid rgba(239, 68, 68, 0.3);
    border-radius: 8px;
    padding: 6px 12px;
    font-size: 13px;
    cursor: pointer;
    transition: all 0.2s;
  }
  .clear-btn:hover {
    background: rgba(239, 68, 68, 0.2);
  }

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

  /* 流式输出光标动画 */
  .streaming-cursor::after {
    content: '▊';
    animation: blink 0.8s infinite;
    color: #8b5cf6;
    margin-left: 1px;
  }

  @keyframes blink {
    0%, 100% { opacity: 1; }
    50% { opacity: 0; }
  }

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
    <h1>Agent 智能对话平台</h1>
    <div class="subtitle">流式输出 · 上下文记忆</div>
  </div>
  <div class="controls">
    <select id="modelSelect">
      <option value="kimi-k2">Kimi K2</option>
      <option value="glm-4">智谱 GLM-4</option>
    </select>
    <button class="clear-btn" onclick="clearHistory()">清空记忆</button>
  </div>
</div>

<div class="capabilities">
  <span class="cap-tag">💬 智能对话</span>
  <span class="cap-tag">🧮 数学计算</span>
  <span class="cap-tag">🕐 时间查询</span>
  <span class="cap-tag">⚡ 流式输出</span>
</div>

<div class="messages" id="messages">
  <div class="welcome" id="welcome">
    <div class="icon">✨</div>
    <h2>你好，有什么可以帮你的？</h2>
    <p>我是全能 AI 助手，带有记忆功能。你可以随时切换模型，或者清空对话记忆。</p>
    <div class="suggestions">
      <div class="suggestion" onclick="sendSuggestion(this)">现在几点了？</div>
      <div class="suggestion" onclick="sendSuggestion(this)">帮我算 1024 × 768</div>
      <div class="suggestion" onclick="sendSuggestion(this)">介绍一下你的大语言模型</div>
      <div class="suggestion" onclick="sendSuggestion(this)">刚才的问题你还记得吗？</div>
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
const modelSelect = document.getElementById('modelSelect');
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

async function clearHistory() {
  if (!confirm("确定要清空服务端的所有对话记忆吗？")) return;
  
  try {
    await fetch('/api/chat/clear', { method: 'POST' });
    messagesEl.innerHTML = '';
    const clone = welcomeEl.cloneNode(true);
    messagesEl.appendChild(clone);
    clone.id = "welcome"; // keep the ID so it can be removed again
    addMessage("记忆已清空 👋", "agent");
  } catch (e) {
    alert("清空失败: " + e.message);
  }
}

function addMessage(text, role, imageUrl) {
  const currentWelcome = document.getElementById('welcome');
  if (currentWelcome) currentWelcome.style.display = 'none';

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
  return bubble;
}

// 创建空的 agent 消息气泡（用于流式填充）
function addStreamBubble() {
  const currentWelcome = document.getElementById('welcome');
  if (currentWelcome) currentWelcome.style.display = 'none';

  const div = document.createElement('div');
  div.className = 'msg agent';

  const avatar = document.createElement('div');
  avatar.className = 'avatar';
  avatar.textContent = '🤖';

  const bubble = document.createElement('div');
  bubble.className = 'bubble streaming-cursor';

  div.appendChild(avatar);
  div.appendChild(bubble);
  messagesEl.appendChild(div);
  messagesEl.scrollTop = messagesEl.scrollHeight;
  return bubble;
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

  const selectedModel = modelSelect.value;

  isLoading = true;
  sendBtn.disabled = true;
  inputEl.value = '';

  addMessage(text, 'user');
  showTyping();

  try {
    const resp = await fetch('/api/chat/stream', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ message: text, model: selectedModel })
    });

    if (!resp.ok) {
      removeTyping();
      // fetch backend text for detail if bad request
      let errDetail = resp.statusText;
      try {
          errDetail = await resp.text();
      } catch (e) {}
      addMessage('❌ 服务器错误: ' + resp.status + " - " + errDetail, 'error');
      isLoading = false;
      sendBtn.disabled = false;
      inputEl.focus();
      return;
    }

    removeTyping();
    const bubble = addStreamBubble();

    const reader = resp.body.getReader();
    const decoder = new TextDecoder('utf-8');
    let buffer = '';

    while (true) {
      const { done, value } = await reader.read();
      if (done) break;

      buffer += decoder.decode(value, { stream: true });

      // 按行解析 SSE
      const lines = buffer.split('\n');
      buffer = lines.pop(); // 保留未完成的行

      for (const line of lines) {
        const trimmed = line.trim();
        if (!trimmed || !trimmed.startsWith('data: ')) continue;

        const data = trimmed.slice(6); // 去掉 "data: "
        if (data === '[DONE]') {
          bubble.classList.remove('streaming-cursor');
          continue;
        }

        try {
          const chunk = JSON.parse(data);

          if (chunk.error) {
            bubble.textContent = '❌ ' + chunk.error;
            bubble.classList.remove('streaming-cursor');
            continue;
          }

          if (chunk.content) {
            bubble.textContent += chunk.content;
            messagesEl.scrollTop = messagesEl.scrollHeight;
          }

          if (chunk.image) {
            const img = document.createElement('img');
            img.src = chunk.image;
            img.className = 'chat-img';
            img.onclick = () => window.open(chunk.image, '_blank');
            bubble.appendChild(img);
          }
        } catch (e) {
          // 忽略解析错误
        }
      }
    }

    bubble.classList.remove('streaming-cursor');

  } catch (err) {
    removeTyping();
    addMessage('❌ 网络错误: ' + err.message, 'error');
  }

  isLoading = false;
  sendBtn.disabled = false;
  inputEl.focus();
}

inputEl.focus();
</script>
</body>
</html>`
