package main

import (
	"context"
	"fmt"
	"io/ioutil"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"unicode"

	"github.com/cloudwego/eino-ext/components/embedding/openai"
)

// DocumentStore 表示内存中的向量库 + BM25 索引
type DocumentStore struct {
	Vectors [][]float64
	Texts   []string
	mu      sync.RWMutex

	// BM25 索引
	bm25Ready bool
	termFreqs []map[string]int // 每个文档的词频表
	docFreqs  map[string]int   // 每个词出现在多少篇文档中
	docLens   []int            // 每个文档的词数
	avgDocLen float64
	totalDocs int
}

var GlobalRAGStore *DocumentStore
var embedder *openai.Embedder

// InitRAG 初始化 RAG 模块，读取知识库并建立索引
func InitRAG(ctx context.Context, apiKey string) error {
	GlobalRAGStore = &DocumentStore{
		Vectors:  make([][]float64, 0),
		Texts:    make([]string, 0),
		docFreqs: make(map[string]int),
	}

	// 0. 连接 PostgreSQL + pgvector（环境变量 PGVECTOR_DSN 存在时启用）
	if dsn := os.Getenv("PGVECTOR_DSN"); dsn != "" {
		if err := InitPgVector(ctx, dsn); err != nil {
			fmt.Printf("⚠️  pgvector 连接失败: %v，将仅使用内存向量+BM25。\n", err)
		}
	}

	// 1. 初始化智谱 Embedding 客户端（Key 为空时跳过，BM25 仍可用）
	var err error
	if apiKey == "" {
		fmt.Println("⚠️  InitRAG 发现 API Key 为空，向量检索不可用，将仅使用 BM25 检索。")
	} else {
		embedder, err = openai.NewEmbedder(ctx, &openai.EmbeddingConfig{
			APIKey:  apiKey,
			BaseURL: "https://open.bigmodel.cn/api/paas/v4", // 智谱 API
			Model:   "embedding-3",                          // 智谱 embedding 模型
		})
		if err != nil {
			fmt.Printf("⚠️  初始化 Embedder 失败: %v，将仅使用 BM25 检索。\n", err)
			embedder = nil
		}
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

	var pgTableCreated bool

	const batchSize = 20
	for i := 0; i < len(allChunks); i += batchSize {
		end := i + batchSize
		if end > len(allChunks) {
			end = len(allChunks)
		}

		batchTexts := allChunks[i:end]
		if embedder != nil {
			embeddings, err := embedder.EmbedStrings(ctx, batchTexts)
			if err != nil {
				fmt.Printf("⚠️  获取 Embedding 失败 (批次 %d-%d): %v\n", i, end, err)
				// embedding 失败时仍保存文本，BM25 照常工作
				GlobalRAGStore.mu.Lock()
				GlobalRAGStore.Texts = append(GlobalRAGStore.Texts, batchTexts...)
				GlobalRAGStore.mu.Unlock()
				continue
			}
			GlobalRAGStore.mu.Lock()
			GlobalRAGStore.Texts = append(GlobalRAGStore.Texts, batchTexts...)
			GlobalRAGStore.Vectors = append(GlobalRAGStore.Vectors, embeddings...)
			GlobalRAGStore.mu.Unlock()

			// pgvector: 首批获取维度后建表，随后批量插入
			if pgDB != nil && !pgTableCreated && len(embeddings) > 0 {
				if err := EnsurePgVectorTable(ctx, len(embeddings[0])); err != nil {
					fmt.Printf("⚠️  pgvector 建表失败: %v\n", err)
				} else {
					pgTableCreated = true
				}
			}
			if pgTableCreated {
				if err := InsertPgVector(ctx, batchTexts, embeddings); err != nil {
					fmt.Printf("⚠️  pgvector 插入失败 (批次 %d-%d): %v\n", i, end, err)
				}
			}
		} else {
			GlobalRAGStore.mu.Lock()
			GlobalRAGStore.Texts = append(GlobalRAGStore.Texts, batchTexts...)
			GlobalRAGStore.mu.Unlock()
		}
	}

	// 5. 构建 BM25 倒排索引（纯本地，无需 API）
	GlobalRAGStore.BuildBM25Index()

	fmt.Printf("✅ RAG 知识库构建完成，共索引 %d 个文本块（向量+BM25 混合检索）。\n", len(GlobalRAGStore.Texts))
	return nil
}

// scoreItem 用于内部排序
type scoreItem struct {
	index int
	score float64
}

// Search 执行混合检索：向量相似度 + BM25，RRF 融合排序
func (s *DocumentStore) Search(ctx context.Context, query string, topK int) ([]string, error) {
	s.mu.RLock()
	texts := make([]string, len(s.Texts))
	copy(texts, s.Texts)
	vectors := make([][]float64, len(s.Vectors))
	copy(vectors, s.Vectors)
	bm25Ready := s.bm25Ready
	s.mu.RUnlock()

	if len(texts) == 0 {
		return nil, nil
	}

	// 1. 向量检索（优先 pgvector，未启用则回退内存遍历）
	vecRankMap := make(map[int]int) // docIndex -> rank
	if embedder != nil {
		queryVecs, err := embedder.EmbedStrings(ctx, []string{query})
		if err == nil && len(queryVecs) > 0 {
			qVec := queryVecs[0]

			if pgvectorOK {
				// 使用 pgvector 外部向量库检索
				ids, pgErr := SearchPgVector(ctx, qVec, topK*2)
				if pgErr == nil {
					for rank, id := range ids {
						// PG SERIAL id 从 1 开始，对应 Texts 下标需减 1
						vecRankMap[id-1] = rank + 1
					}
				} else {
					fmt.Printf("⚠️  pgvector 检索失败: %v，回退内存向量检索。\n", pgErr)
				}
			}

			// pgvector 未启用或失败时，回退内存遍历
			if len(vecRankMap) == 0 && len(vectors) == len(texts) {
				var scores []scoreItem
				for i, vec := range vectors {
					scores = append(scores, scoreItem{index: i, score: cosineSimilarity(qVec, vec)})
				}
				sort.Slice(scores, func(i, j int) bool { return scores[i].score > scores[j].score })
				for rank, si := range scores {
					if si.score < 0.2 {
						break
					}
					vecRankMap[si.index] = rank + 1
				}
			}
		}
	}

	// 2. BM25 关键词检索（纯本地，无需 API）
	bm25RankMap := make(map[int]int)
	if bm25Ready {
		bm25RankMap = s.bm25Search(query, topK*3)
	}

	// 3. RRF 倒数排名融合：score = Σ 1/(k+rank)，k=60
	const k = 60.0
	rrfScores := make(map[int]float64)
	for idx, rank := range vecRankMap {
		rrfScores[idx] += 1.0 / (k + float64(rank))
	}
	for idx, rank := range bm25RankMap {
		rrfScores[idx] += 1.0 / (k + float64(rank))
	}

	if len(rrfScores) == 0 {
		return nil, nil
	}

	type rrfItem struct {
		index int
		score float64
	}
	var items []rrfItem
	for idx, score := range rrfScores {
		items = append(items, rrfItem{index: idx, score: score})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].score > items[j].score })

	var results []string
	for i := 0; i < topK && i < len(items); i++ {
		results = append(results, texts[items[i].index])
	}
	return results, nil
}

// BuildBM25Index 基于已加载的 Texts 构建 BM25 倒排索引
func (s *DocumentStore) BuildBM25Index() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(s.Texts) == 0 {
		return
	}

	s.totalDocs = len(s.Texts)
	s.termFreqs = make([]map[string]int, s.totalDocs)
	s.docLens = make([]int, s.totalDocs)
	s.docFreqs = make(map[string]int)

	for i, text := range s.Texts {
		terms := tokenize(text)
		s.docLens[i] = len(terms)
		tf := make(map[string]int)
		seen := make(map[string]bool)
		for _, t := range terms {
			tf[t]++
			if !seen[t] {
				seen[t] = true
				s.docFreqs[t]++
			}
		}
		s.termFreqs[i] = tf
	}

	var totalLen int
	for _, l := range s.docLens {
		totalLen += l
	}
	s.avgDocLen = float64(totalLen) / float64(s.totalDocs)
	s.bm25Ready = true
	fmt.Printf("📑 BM25 索引构建完成，词典大小: %d 词。\n", len(s.docFreqs))
}

// bm25Search 执行 BM25 检索，返回 docIndex -> rank 映射
func (s *DocumentStore) bm25Search(query string, topK int) map[int]int {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if !s.bm25Ready {
		return nil
	}

	terms := tokenize(query)
	if len(terms) == 0 {
		return nil
	}

	const k1 = 1.5
	const b = 0.75

	var scores []scoreItem
	for i := range s.Texts {
		score := 0.0
		docLen := float64(s.docLens[i])
		for _, term := range terms {
			df, ok := s.docFreqs[term]
			if !ok {
				continue
			}
			// IDF (Robertson/Sparck Jones)
			idf := math.Log((float64(s.totalDocs)-float64(df)+0.5)/(float64(df)+0.5)) + 1.0
			tf := float64(s.termFreqs[i][term])
			score += idf * (tf * (k1 + 1)) / (tf + k1*(1-b+b*docLen/s.avgDocLen))
		}
		if score > 0 {
			scores = append(scores, scoreItem{index: i, score: score})
		}
	}

	if len(scores) == 0 {
		return nil
	}

	sort.Slice(scores, func(i, j int) bool { return scores[i].score > scores[j].score })

	rankMap := make(map[int]int)
	for rank, si := range scores {
		if rank >= topK {
			break
		}
		rankMap[si.index] = rank + 1
	}
	return rankMap
}

// tokenize 简单分词：小写 + 去标点 + 按空格/unicode分词
func tokenize(text string) []string {
	text = strings.ToLower(text)
	var terms []string
	var buf strings.Builder
	for _, r := range text {
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			buf.WriteRune(r)
		} else {
			if buf.Len() > 0 {
				terms = append(terms, buf.String())
				buf.Reset()
			}
		}
	}
	if buf.Len() > 0 {
		terms = append(terms, buf.String())
	}
	return terms
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
