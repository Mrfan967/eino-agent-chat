package logic

import (
	"context"
	"sort"
	"time"

	einoembedding "github.com/cloudwego/eino-ext/components/embedding/openai"

	"awesomeProject/rag-rpc/awesomeProject/rag-rpc/rag"
	"awesomeProject/rag-rpc/internal/lib"
	"awesomeProject/rag-rpc/internal/svc"

	"github.com/zeromicro/go-zero/core/logx"
)

type SearchLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewSearchLogic(ctx context.Context, svcCtx *svc.ServiceContext) *SearchLogic {
	return &SearchLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// 混合检索（向量+BM25，RRF融合）
func (l *SearchLogic) Search(in *rag.SearchReq) (*rag.SearchResp, error) {
	start := time.Now()
	topK := int(in.TopK)
	if topK <= 0 {
		topK = 5
	}

	texts, vectors, bm25Ready := l.svcCtx.RAG.Store.Snapshot()
	if len(texts) == 0 {
		return &rag.SearchResp{Chunks: []string{}}, nil
	}

	type scoreItem struct {
		index int
		score float64
	}

	// 1. 向量检索
	vecRankMap := make(map[int]int)
	if embedder, ok := l.svcCtx.RAG.Embedder.(*einoembedding.Embedder); ok && embedder != nil {
		queryVecs, err := embedder.EmbedStrings(l.ctx, []string{in.Query})
		if err == nil && len(queryVecs) > 0 {
			qVec := queryVecs[0]

			if len(vectors) == len(texts) {
				var scores []scoreItem
				for i, vec := range vectors {
					scores = append(scores, scoreItem{index: i, score: lib.CosineSimilarity(qVec, vec)})
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
		bm25RankMap = l.svcCtx.RAG.Store.BM25Search(in.Query, topK*3)
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
		return &rag.SearchResp{Chunks: []string{}}, nil
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

	return &rag.SearchResp{
		Chunks:       results,
		SearchTimeMs: float32(time.Since(start).Milliseconds()),
	}, nil
}
