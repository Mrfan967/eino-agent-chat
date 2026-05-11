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

type VectorSearchLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewVectorSearchLogic(ctx context.Context, svcCtx *svc.ServiceContext) *VectorSearchLogic {
	return &VectorSearchLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// 仅向量检索
func (l *VectorSearchLogic) VectorSearch(in *rag.VectorSearchReq) (*rag.SearchResp, error) {
	start := time.Now()
	topK := int(in.TopK)
	if topK <= 0 {
		topK = 5
	}

	texts, vectors, _ := l.svcCtx.RAG.Store.Snapshot()
	if len(texts) == 0 || len(vectors) != len(texts) {
		return &rag.SearchResp{Chunks: []string{}}, nil
	}

	embedder, ok := l.svcCtx.RAG.Embedder.(*einoembedding.Embedder)
	if !ok || embedder == nil {
		return &rag.SearchResp{Chunks: []string{}}, nil
	}

	queryVecs, err := embedder.EmbedStrings(l.ctx, []string{in.Query})
	if err != nil || len(queryVecs) == 0 {
		return &rag.SearchResp{Chunks: []string{}}, nil
	}
	qVec := queryVecs[0]

	type scoreItem struct {
		index int
		score float64
	}
	var scores []scoreItem
	for i, vec := range vectors {
		sim := lib.CosineSimilarity(qVec, vec)
		if sim >= 0.2 {
			scores = append(scores, scoreItem{index: i, score: sim})
		}
	}
	sort.Slice(scores, func(i, j int) bool { return scores[i].score > scores[j].score })

	var results []string
	for i := 0; i < topK && i < len(scores); i++ {
		results = append(results, texts[scores[i].index])
	}

	return &rag.SearchResp{
		Chunks:       results,
		SearchTimeMs: float32(time.Since(start).Milliseconds()),
	}, nil
}
