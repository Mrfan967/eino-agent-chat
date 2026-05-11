package lib

import (
	"fmt"
	"math"
	"sort"
	"sync"
)

// DocumentStore 内存向量库 + BM25 索引
type DocumentStore struct {
	Vectors [][]float64
	Texts   []string
	mu      sync.RWMutex

	bm25Ready bool
	termFreqs []map[string]int
	docFreqs  map[string]int
	docLens   []int
	avgDocLen float64
	totalDocs int
}

// NewDocumentStore 创建空的文档库
func NewDocumentStore() *DocumentStore {
	return &DocumentStore{
		Vectors:  make([][]float64, 0),
		Texts:    make([]string, 0),
		docFreqs: make(map[string]int),
	}
}

// AddBatch 线程安全地追加一批文本和向量
func (s *DocumentStore) AddBatch(texts []string, vectors [][]float64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Texts = append(s.Texts, texts...)
	if len(vectors) > 0 {
		s.Vectors = append(s.Vectors, vectors...)
	}
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
		terms := Tokenize(text)
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

// BM25Search 执行 BM25 检索，返回 docIndex -> rank 映射
func (s *DocumentStore) BM25Search(query string, topK int) map[int]int {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if !s.bm25Ready {
		return nil
	}

	terms := Tokenize(query)
	if len(terms) == 0 {
		return nil
	}

	const k1 = 1.5
	const b = 0.75

	type scoreItem struct {
		index int
		score float64
	}

	var scores []scoreItem
	for i := range s.Texts {
		score := 0.0
		docLen := float64(s.docLens[i])
		for _, term := range terms {
			df, ok := s.docFreqs[term]
			if !ok {
				continue
			}
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

// Snapshot 线程安全地返回当前 texts 和 vectors 的拷贝
func (s *DocumentStore) Snapshot() (texts []string, vectors [][]float64, bm25Ready bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	texts = make([]string, len(s.Texts))
	copy(texts, s.Texts)
	vectors = make([][]float64, len(s.Vectors))
	copy(vectors, s.Vectors)
	bm25Ready = s.bm25Ready
	return
}

// CosineSimilarity 计算余弦相似度
func CosineSimilarity(a, b []float64) float64 {
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

// ChunkText 文本分块
func ChunkText(text string, chunkSize, overlap int) []string {
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
