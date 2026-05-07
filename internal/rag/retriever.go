package rag

import (
	"context"
	"fmt"
	"io/ioutil"
	"math"
	"path/filepath"
	"sort"
	"strings"

	einoembedding "github.com/cloudwego/eino-ext/components/embedding/openai"

	"awesomeProject/internal/store"
)

// Service 封装 RAG 检索服务
type Service struct {
	store    *DocumentStore
	embedder *einoembedding.Embedder
}

// NewService 创建并初始化 RAG 服务（读取知识库、构建索引）
func NewService(ctx context.Context, apiKey string) (*Service, error) {
	svc := &Service{
		store: NewDocumentStore(),
	}

	// 初始化 Embedder
	if apiKey == "" {
		fmt.Println("⚠️  InitRAG 发现 API Key 为空，向量检索不可用，将仅使用 BM25 检索。")
	} else {
		emb, err := einoembedding.NewEmbedder(ctx, &einoembedding.EmbeddingConfig{
			APIKey:  apiKey,
			BaseURL: "https://open.bigmodel.cn/api/paas/v4",
			Model:   "embedding-3",
		})
		if err != nil {
			fmt.Printf("⚠️  初始化 Embedder 失败: %v，将仅使用 BM25 检索。\n", err)
		} else {
			svc.embedder = emb
		}
	}

	// 读取 knowledge 目录
	files, err := filepath.Glob("knowledge/*.txt")
	if err != nil || len(files) == 0 {
		fmt.Println("ℹ️  knowledge 目录下没有找到 .txt 文件，RAG 知识库为空。")
		return svc, nil
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
		chunks := chunkText(text, 400, 50)
		for _, chunk := range chunks {
			allChunks = append(allChunks, fmt.Sprintf("[来源: %s]\n%s", filepath.Base(file), chunk))
		}
	}

	if len(allChunks) == 0 {
		return svc, nil
	}

	fmt.Printf("🔄 正在为 %d 个文本块生成特征向量...\n", len(allChunks))

	var pgTableCreated bool
	const batchSize = 20

	for i := 0; i < len(allChunks); i += batchSize {
		end := i + batchSize
		if end > len(allChunks) {
			end = len(allChunks)
		}
		batchTexts := allChunks[i:end]

		if svc.embedder != nil {
			embeddings, err := svc.embedder.EmbedStrings(ctx, batchTexts)
			if err != nil {
				fmt.Printf("⚠️  获取 Embedding 失败 (批次 %d-%d): %v\n", i, end, err)
				svc.store.AddBatch(batchTexts, nil)
				continue
			}
			svc.store.AddBatch(batchTexts, embeddings)

			if store.DB != nil && !pgTableCreated && len(embeddings) > 0 {
				if err := store.EnsurePgVectorTable(ctx, len(embeddings[0])); err != nil {
					fmt.Printf("⚠️  pgvector 建表失败: %v\n", err)
				} else {
					pgTableCreated = true
				}
			}
			if pgTableCreated {
				if err := store.InsertPgVector(ctx, batchTexts, embeddings); err != nil {
					fmt.Printf("⚠️  pgvector 插入失败 (批次 %d-%d): %v\n", i, end, err)
				}
			}
		} else {
			svc.store.AddBatch(batchTexts, nil)
		}
	}

	svc.store.BuildBM25Index()
	fmt.Printf("✅ RAG 知识库构建完成，共索引 %d 个文本块（向量+BM25 混合检索）。\n", len(svc.store.Texts))
	return svc, nil
}

// Search 执行混合检索：向量相似度 + BM25，RRF 融合排序
func (s *Service) Search(ctx context.Context, query string, topK int) ([]string, error) {
	texts, vectors, bm25Ready := s.store.Snapshot()

	if len(texts) == 0 {
		return nil, nil
	}

	type scoreItem struct {
		index int
		score float64
	}

	// 1. 向量检索
	vecRankMap := make(map[int]int)
	if s.embedder != nil {
		queryVecs, err := s.embedder.EmbedStrings(ctx, []string{query})
		if err == nil && len(queryVecs) > 0 {
			qVec := queryVecs[0]

			if store.PgVectorOK {
				ids, pgErr := store.SearchPgVector(ctx, qVec, topK*2)
				if pgErr == nil {
					for rank, id := range ids {
						vecRankMap[id-1] = rank + 1
					}
				} else {
					fmt.Printf("⚠️  pgvector 检索失败: %v，回退内存向量检索。\n", pgErr)
				}
			}

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

	// 2. BM25 检索
	bm25RankMap := make(map[int]int)
	if bm25Ready {
		bm25RankMap = s.store.BM25Search(query, topK*3)
	}

	// 3. RRF 融合
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

// IsEmpty 检查知识库是否为空
func (s *Service) IsEmpty() bool {
	return len(s.store.Texts) == 0
}

// --- 内部工具函数 ---

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
		if chunkSize <= overlap {
			break
		}
	}
	return chunks
}
