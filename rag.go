package main

import (
	"context"
	"fmt"
	"io/ioutil"
	"math"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/cloudwego/eino-ext/components/embedding/openai"
)

// DocumentStore 表示内存中的向量库
type DocumentStore struct {
	Vectors [][]float64
	Texts   []string
	mu      sync.RWMutex
}

var GlobalRAGStore *DocumentStore
var embedder *openai.Embedder

// InitRAG 初始化 RAG 模块，读取知识库并建立索引
func InitRAG(ctx context.Context, apiKey string) error {
	GlobalRAGStore = &DocumentStore{
		Vectors: make([][]float64, 0),
		Texts:   make([]string, 0),
	}

	if apiKey == "" {
		fmt.Println("⚠️  InitRAG 发现 API Key 为空，RAG 功能可能无法正常工作。")
		return nil
	}

	// 1. 初始化智谱 Embedding 客户端
	var err error
	embedder, err = openai.NewEmbedder(ctx, &openai.EmbeddingConfig{
		APIKey:  apiKey,
		BaseURL: "https://open.bigmodel.cn/api/paas/v4", // 智谱 API
		Model:   "embedding-3",                          // 智谱 embedding 模型
	})
	if err != nil {
		return fmt.Errorf("初始化 Embedder 失败: %v", err)
	}

	// 2. 读取 knowledge 目录下的所有 txt 文件
	files, err := filepath.Glob("knowledge/*.txt")
	if err != nil || len(files) == 0 {
		fmt.Println("ℹ️  knowledge 目录下没有找到 .txt 文件，RAG 知识库为空。")
		return nil
	}

	fmt.Printf("📚 开始构建 RAG 知识库，找到 %d 个文档...\n", len(files))

	var allChunks []string

	for _, file := range files {
		content, err := ioutil.ReadFile(file)
		if err != nil {
			fmt.Printf("读取文件 %s 失败: %v\n", file, err)
			continue
		}

		text := string(content)
		if strings.TrimSpace(text) == "" {
			continue
		}

		// 3. 简单的文本切片 (Chunking)，每段约 400 字符
		chunks := chunkText(text, 400, 50)
		for _, chunk := range chunks {
			chunkWithMeta := fmt.Sprintf("[来源: %s]\n%s", filepath.Base(file), chunk)
			allChunks = append(allChunks, chunkWithMeta)
		}
	}

	if len(allChunks) == 0 {
		return nil
	}

	// 4. 批量调用 Embedding API (避免单次请求过大，这里按最多 20 个一批)
	fmt.Printf("🔄 正在为 %d 个文本块生成特征向量...\n", len(allChunks))

	const batchSize = 20
	for i := 0; i < len(allChunks); i += batchSize {
		end := i + batchSize
		if end > len(allChunks) {
			end = len(allChunks)
		}

		batchTexts := allChunks[i:end]
		embeddings, err := embedder.EmbedStrings(ctx, batchTexts)
		if err != nil {
			fmt.Printf("⚠️  获取 Embedding 失败 (批次 %d-%d): %v\n", i, end, err)
			continue
		}

		GlobalRAGStore.mu.Lock()
		GlobalRAGStore.Texts = append(GlobalRAGStore.Texts, batchTexts...)
		GlobalRAGStore.Vectors = append(GlobalRAGStore.Vectors, embeddings...)
		GlobalRAGStore.mu.Unlock()
	}

	fmt.Printf("✅ RAG 知识库构建完成，共索引 %d 个文本块。\n", len(GlobalRAGStore.Texts))
	return nil
}

// Search 执行纯内存的向量相似度检索
func (s *DocumentStore) Search(ctx context.Context, query string, topK int) ([]string, error) {
	if embedder == nil || len(s.Vectors) == 0 {
		return nil, nil // 无数据或未初始化
	}

	// 1. 将查询文本向量化
	queryVecs, err := embedder.EmbedStrings(ctx, []string{query})
	if err != nil || len(queryVecs) == 0 {
		return nil, fmt.Errorf("查询向量化失败: %v", err)
	}
	qVec := queryVecs[0]

	s.mu.RLock()
	defer s.mu.RUnlock()

	// 2. 计算并排序余弦相似度
	type scoreItem struct {
		index int
		score float64
	}
	var scores []scoreItem

	for i, vec := range s.Vectors {
		sim := cosineSimilarity(qVec, vec)
		scores = append(scores, scoreItem{index: i, score: sim})
	}

	sort.Slice(scores, func(i, j int) bool {
		return scores[i].score > scores[j].score // 降序
	})

	// 3. 返回 TopK (并且过滤掉相似度太低的)
	var results []string
	for i := 0; i < topK && i < len(scores); i++ {
		if scores[i].score < 0.2 { // 余弦相似度阈值
			break
		}
		results = append(results, s.Texts[scores[i].index])
	}

	return results, nil
}

// cosineSimilarity 计算两个向量的余弦相似度
func cosineSimilarity(a, b []float64) float64 {
	var dotProduct, normA, normB float64
	for i := 0; i < len(a) && i < len(b); i++ {
		dotProduct += a[i] * b[i]
		normA += a[i] * a[i]
		normB += b[i] * b[i]
	}
	if normA == 0 || normB == 0 {
		return 0
	}
	return dotProduct / (math.Sqrt(normA) * math.Sqrt(normB))
}

// chunkText 按字符数切分文本，包含重叠部分
func chunkText(text string, chunkSize, overlap int) []string {
	var chunks []string
	runes := []rune(text)
	length := len(runes)

	if length <= chunkSize {
		return []string{text}
	}

	for i := 0; i < length; {
		end := i + chunkSize
		if end > length {
			end = length
		}
		chunks = append(chunks, string(runes[i:end]))
		i += chunkSize - overlap
		if i < 0 {
			i = 0
		}
		// 防止死循环
		if chunkSize <= overlap {
			break
		}
	}
	return chunks
}
