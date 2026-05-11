package logic

import (
	"context"
	"time"

	"awesomeProject/rag-rpc/awesomeProject/rag-rpc/rag"
	"awesomeProject/rag-rpc/internal/svc"

	"github.com/zeromicro/go-zero/core/logx"
)

type BM25SearchLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewBM25SearchLogic(ctx context.Context, svcCtx *svc.ServiceContext) *BM25SearchLogic {
	return &BM25SearchLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// 仅BM25检索
func (l *BM25SearchLogic) BM25Search(in *rag.BM25SearchReq) (*rag.SearchResp, error) {
	start := time.Now()
	topK := int(in.TopK)
	if topK <= 0 {
		topK = 5
	}

	texts, _, bm25Ready := l.svcCtx.RAG.Store.Snapshot()
	if len(texts) == 0 || !bm25Ready {
		return &rag.SearchResp{Chunks: []string{}}, nil
	}

	bm25RankMap := l.svcCtx.RAG.Store.BM25Search(in.Query, topK)
	if len(bm25RankMap) == 0 {
		return &rag.SearchResp{Chunks: []string{}}, nil
	}

	// 按 rank 排序
	type rankItem struct {
		index int
		rank  int
	}
	var items []rankItem
	for idx, rank := range bm25RankMap {
		items = append(items, rankItem{index: idx, rank: rank})
	}
	for i := 0; i < len(items)-1; i++ {
		for j := i + 1; j < len(items); j++ {
			if items[i].rank > items[j].rank {
				items[i], items[j] = items[j], items[i]
			}
		}
	}

	var results []string
	for i := 0; i < topK && i < len(items); i++ {
		results = append(results, texts[items[i].index])
	}

	return &rag.SearchResp{
		Chunks:       results,
		SearchTimeMs: float32(time.Since(start).Milliseconds()),
	}, nil
}
