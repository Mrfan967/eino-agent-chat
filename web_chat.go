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

type chatRequest struct {
	Message   string `json:"message"`
	Model     string `json:"model"`
	SessionID string `json:"session_id"`
}

type streamChunk struct {
	Content string `json:"content,omitempty"`
	Error   string `json:"error,omitempty"`
	Done    bool   `json:"done,omitempty"`
	Image   string `json:"image,omitempty"`
}

type promptConfig struct {
	Persona     string   `json:"persona"`
	SystemRules []string `json:"system_rules"`
	Rules       []rule   `json:"rules"`
	Fallback    string   `json:"fallback"`
}

type rule struct {
	Trigger string `json:"trigger"`
	Answer  string `json:"answer"`
	Image   string `json:"image"`
}

var (
	agentCache       = make(map[string]*react.Agent)
	agentMutex       sync.Mutex
	sessionHistories = make(map[string][]*schema.Message)
	historyMutex     sync.Mutex
	configuredTools  []tool.BaseTool
	configuredPrompt string
)

func StartWebChat() {
	ctx := context.Background()

	promptData, err := readPromptConfig()
	if err != nil {
		fmt.Printf("⚠️  读取 prompt_config.json 失败: %v，将使用默认提示\n", err)
		promptData = &promptConfig{
			Persona: "你是人工智能助手，擅长中文和英文的对话。",
		}
	}

	if err := initRuntime(ctx, promptData); err != nil {
		fmt.Printf("初始化运行环境失败: %v\n", err)
		return
	}

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

		modelID := normalizeModelID(req.Model)
		agent, err := getOrCreateAgent(r.Context(), modelID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("X-Accel-Buffering", "no")

		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "不支持流式输出", http.StatusInternalServerError)
			return
		}

		sid := req.SessionID
		if sid == "" {
			sid = "default"
		}

		userMsg := schema.UserMessage(req.Message)

		historyMutex.Lock()
		history := append(sessionHistories[sid], userMsg)
		sessionHistories[sid] = history
		messages := make([]*schema.Message, len(history))
		copy(messages, history)
		historyMutex.Unlock()

		streamResult, err := agent.Stream(r.Context(), messages)
		if err != nil {
			chunk, _ := json.Marshal(streamChunk{Error: fmt.Sprintf("Agent 出错: %v", err)})
			fmt.Fprintf(w, "data: %s\n\n", chunk)
			flusher.Flush()
			fmt.Fprintf(w, "data: [DONE]\n\n")
			flusher.Flush()

			historyMutex.Lock()
			if h := sessionHistories[sid]; len(h) > 0 {
				sessionHistories[sid] = h[:len(h)-1]
			}
			historyMutex.Unlock()
			return
		}

		var fullContent strings.Builder

		for {
			msg, err := streamResult.Recv()
			if err != nil {
				break
			}
			if msg == nil || msg.Content == "" {
				continue
			}

			fullContent.WriteString(msg.Content)

			chunk, _ := json.Marshal(streamChunk{Content: msg.Content})
			fmt.Fprintf(w, "data: %s\n\n", chunk)
			flusher.Flush()
		}

		finalReply := fullContent.String()
		if finalReply != "" {
			historyMutex.Lock()
			sessionHistories[sid] = append(sessionHistories[sid], schema.AssistantMessage(finalReply, nil))
			historyMutex.Unlock()
		}

		imageURL := matchImage(promptData, req.Message, finalReply)
		if imageURL != "" {
			chunk, _ := json.Marshal(streamChunk{Image: imageURL})
			fmt.Fprintf(w, "data: %s\n\n", chunk)
			flusher.Flush()
		}

		fmt.Fprintf(w, "data: [DONE]\n\n")
		flusher.Flush()
	})

	http.HandleFunc("/api/chat/clear", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
			return
		}

		var req struct {
			SessionID string `json:"session_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "请求格式错误", http.StatusBadRequest)
			return
		}

		historyMutex.Lock()
		delete(sessionHistories, req.SessionID)
		historyMutex.Unlock()

		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status":"ok"}`))
	})

	http.Handle("/image/", http.StripPrefix("/image/", http.FileServer(http.Dir("image"))))

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(chatHTML))
	})

	addr := ":8080"
	fmt.Println("\n🌐 Web 对话服务已启动！(支持多模型 + RAG + 上下文记忆)")
	fmt.Println("   浏览器访问: http://localhost" + addr)
	fmt.Println("   按 Ctrl+C 停止服务\n")

	openBrowser("http://localhost" + addr)

	if err := http.ListenAndServe(addr, nil); err != nil {
		fmt.Printf("服务启动失败: %v\n", err)
	}
}

func initRuntime(ctx context.Context, cfg *promptConfig) error {
	configuredTools = []tool.BaseTool{
		&CalculatorTool{},
		&TimeTool{},
		&KnowledgeTool{},
	}
	configuredPrompt = buildSystemPrompt(cfg)

	glmKey := os.Getenv("ZHIPU_API_KEY")
	if glmKey == "" {
		glmKey = os.Getenv("OPENAI_API_KEY")
	}

	if err := InitRAG(ctx, glmKey); err != nil {
		fmt.Printf("⚠️  RAG 知识库初始化失败: %v\n", err)
	} else {
		fmt.Printf("✅ RAG 知识库初始化完成\n")
	}

	return nil
}

func buildSystemPrompt(cfg *promptConfig) string {
	if cfg == nil {
		return "你是智能助手"
	}

	var builder strings.Builder
	builder.WriteString(cfg.Persona)
	builder.WriteString("\n在回答问题时，请遵循以下规则：\n\n")

	for i, r := range cfg.Rules {
		builder.WriteString(fmt.Sprintf("条目%d. 当用户问“%s”或类似问题时，回答：%s\n\n", i+1, r.Trigger, r.Answer))
	}

	if len(cfg.SystemRules) > 0 {
		builder.WriteString("系统全局规则：\n")
		for i, sr := range cfg.SystemRules {
			builder.WriteString(fmt.Sprintf("- 规则%d: %s\n", i+1, sr))
		}
		builder.WriteString("\n")
	}

	if cfg.Fallback != "" {
		builder.WriteString("兜底策略：")
		builder.WriteString(cfg.Fallback)
	}

	return builder.String()
}

func getOrCreateAgent(ctx context.Context, modelID string) (*react.Agent, error) {
	modelID = normalizeModelID(modelID)

	agentMutex.Lock()
	if agent, ok := agentCache[modelID]; ok {
		agentMutex.Unlock()
		return agent, nil
	}
	agentMutex.Unlock()

	chatModel, err := newChatModel(ctx, modelID)
	if err != nil {
		return nil, err
	}

	agent, err := react.NewAgent(ctx, &react.AgentConfig{
		ToolCallingModel: chatModel,
		ToolsConfig:      compose.ToolsNodeConfig{Tools: configuredTools},
		MessageModifier: func(ctx context.Context, input []*schema.Message) []*schema.Message {
			msgs := make([]*schema.Message, 0, len(input)+1)
			msgs = append(msgs, schema.SystemMessage(configuredPrompt))
			msgs = append(msgs, input...)
			return msgs
		},
	})
	if err != nil {
		return nil, fmt.Errorf("创建 Agent 失败: %v", err)
	}

	agentMutex.Lock()
	if cached, ok := agentCache[modelID]; ok {
		agentMutex.Unlock()
		return cached, nil
	}
	agentCache[modelID] = agent
	agentMutex.Unlock()

	return agent, nil
}

func newChatModel(ctx context.Context, modelID string) (*openai.ChatModel, error) {
	modelID = normalizeModelID(modelID)

	switch {
	case strings.HasPrefix(modelID, "moonshot"):
		apiKey := os.Getenv("MOONSHOT_API_KEY")
		if apiKey == "" {
			return nil, fmt.Errorf("模型 %s 缺少 MOONSHOT_API_KEY", modelID)
		}
		return openai.NewChatModel(ctx, &openai.ChatModelConfig{
			APIKey:  apiKey,
			BaseURL: "https://api.moonshot.cn/v1",
			Model:   modelID,
		})

	case strings.HasPrefix(modelID, "glm"):
		apiKey := os.Getenv("ZHIPU_API_KEY")
		if apiKey == "" {
			apiKey = os.Getenv("OPENAI_API_KEY")
		}
		if apiKey == "" {
			return nil, fmt.Errorf("模型 %s 缺少 ZHIPU_API_KEY", modelID)
		}
		return openai.NewChatModel(ctx, &openai.ChatModelConfig{
			APIKey:  apiKey,
			BaseURL: "https://open.bigmodel.cn/api/paas/v4",
			Model:   modelID,
		})

	case strings.HasPrefix(modelID, "qwen"):
		apiKey := os.Getenv("ALIYUN_API_KEY")
		if apiKey == "" {
			return nil, fmt.Errorf("模型 %s 缺少 ALIYUN_API_KEY", modelID)
		}
		return openai.NewChatModel(ctx, &openai.ChatModelConfig{
			APIKey:  apiKey,
			BaseURL: "https://dashscope.aliyuncs.com/compatible-mode/v1",
			Model:   modelID,
		})
	}

	return nil, fmt.Errorf("不支持的模型: %s", modelID)
}

func normalizeModelID(modelID string) string {
	switch modelID {
	case "", "kimi-k2":
		return "moonshot-v1-8k"
	case "glm-4":
		return "glm-4.1v-thinking-flash"
	default:
		return modelID
	}
}

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

func matchImage(cfg *promptConfig, userMsg, reply string) string {
	if cfg != nil {
		msgLower := strings.ToLower(userMsg)
		for _, r := range cfg.Rules {
			triggerLower := strings.ToLower(r.Trigger)
			if r.Image != "" && (strings.Contains(msgLower, triggerLower) || strings.Contains(triggerLower, msgLower)) {
				return "/image/" + r.Image
			}
		}
	}

	replyLower := strings.ToLower(reply)
	if strings.Contains(replyLower, "赵苏通") {
		return "/image/su.jpg"
	}
	if strings.Contains(replyLower, "范晨旭") {
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
    width: 40px;
    height: 40px;
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

  .header .subtitle {
    font-size: 12px;
    color: #a1a1aa;
    margin-top: 2px;
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
    border-bottom: 1px solid rgba(255, 255, 255, 0.04);
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
    width: 36px;
    height: 36px;
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
    border: 1px solid rgba(255, 255, 255, 0.06);
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
    width: 8px;
    height: 8px;
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

  .welcome .icon {
    font-size: 48px;
    margin-bottom: 16px;
  }

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
    width: 44px;
    height: 44px;
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
    <div class="subtitle">流式输出 · 多模型切换 · 上下文记忆</div>
  </div>
  <div class="controls">
    <select id="modelSelect">
      <optgroup label="Moonshot">
        <option value="moonshot-v1-8k" selected>Kimi 8K</option>
      </optgroup>
      <optgroup label="智谱 GLM">
        <option value="glm-4.1v-thinking-flash">GLM-4.1V-Thinking-Flash</option>
        <option value="glm-4-flash">GLM-4-Flash</option>
        <option value="glm-4.7-flash">GLM-4.7-Flash</option>
      </optgroup>
      <optgroup label="通义千问">
        <option value="qwen-turbo">Qwen-Turbo</option>
        <option value="qwen-plus">Qwen-Plus</option>
        <option value="qwen-max">Qwen-Max</option>
      </optgroup>
    </select>
    <button class="clear-btn" onclick="clearHistory()">清空记忆</button>
  </div>
</div>

<div class="capabilities">
  <span class="cap-tag">💬 智能对话</span>
  <span class="cap-tag">🧮 数学计算</span>
  <span class="cap-tag">🕐 时间查询</span>
  <span class="cap-tag">📚 RAG 检索</span>
  <span class="cap-tag">⚡ 流式输出</span>
</div>

<div class="messages" id="messages">
  <div class="welcome" id="welcome">
    <div class="icon">✨</div>
    <h2>你好，有什么可以帮你的？</h2>
    <p>支持多模型切换、知识库检索和上下文记忆。</p>
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
let isLoading = false;

let sessionId = sessionStorage.getItem('sessionId');
if (!sessionId) {
  sessionId = 'sess_' + Math.random().toString(36).slice(2, 11) + Date.now().toString(36);
  sessionStorage.setItem('sessionId', sessionId);
}

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
  if (!confirm('确定要清空当前的对话记忆吗？')) return;

  try {
    await fetch('/api/chat/clear', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ session_id: sessionId })
    });
    messagesEl.innerHTML = '';
    messagesEl.insertAdjacentHTML('beforeend',
      '<div class="welcome" id="welcome">' +
      '<div class="icon">✨</div>' +
      '<h2>你好，有什么可以帮你的？</h2>' +
      '<p>支持多模型切换、知识库检索和上下文记忆。</p>' +
      '<div class="suggestions">' +
      '<div class="suggestion" onclick="sendSuggestion(this)">现在几点了？</div>' +
      '<div class="suggestion" onclick="sendSuggestion(this)">帮我算 1024 × 768</div>' +
      '<div class="suggestion" onclick="sendSuggestion(this)">介绍一下你的大语言模型</div>' +
      '<div class="suggestion" onclick="sendSuggestion(this)">刚才的问题你还记得吗？</div>' +
      '</div>' +
      '</div>'
    );
    addMessage('记忆已清空', 'agent');
  } catch (e) {
    alert('清空失败: ' + e.message);
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

  const selectedModel = document.getElementById('modelSelect').value;

  isLoading = true;
  sendBtn.disabled = true;
  inputEl.value = '';

  addMessage(text, 'user');
  showTyping();

  try {
    const resp = await fetch('/api/chat/stream', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        message: text,
        model: selectedModel,
        session_id: sessionId
      })
    });

    if (!resp.ok) {
      removeTyping();
      let errDetail = resp.statusText;
      try {
        errDetail = await resp.text();
      } catch (e) {}
      addMessage('❌ 服务器错误: ' + resp.status + ' - ' + errDetail, 'error');
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
      const lines = buffer.split('\n');
      buffer = lines.pop();

      for (const line of lines) {
        const trimmed = line.trim();
        if (!trimmed || !trimmed.startsWith('data: ')) continue;

        const data = trimmed.slice(6);
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
        } catch (e) {}
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
