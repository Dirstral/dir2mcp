package retrieval

import (
	"context"
	"math"
	"path"
	"sort"
	"strings"
	"sync"

	"github.com/Dirstral/dir2mcp/internal/model"
)

type Service struct {
	store        model.Store
	textIndex    model.Index
	codeIndex    model.Index
	embedder     model.Embedder
	gen          model.Generator
	textModel    string
	codeModel    string
	metaMu       sync.RWMutex
	chunkByLabel map[uint64]model.SearchHit
}

func NewService(store model.Store, index model.Index, embedder model.Embedder, gen model.Generator) *Service {
	return &Service{
		store:        store,
		textIndex:    index,
		codeIndex:    index,
		embedder:     embedder,
		gen:          gen,
		textModel:    "mistral-embed",
		codeModel:    "codestral-embed",
		chunkByLabel: make(map[uint64]model.SearchHit),
	}
}

func (s *Service) SetQueryEmbeddingModel(modelName string) {
	if strings.TrimSpace(modelName) == "" {
		return
	}
	s.textModel = modelName
}

func (s *Service) SetCodeEmbeddingModel(modelName string) {
	if strings.TrimSpace(modelName) == "" {
		return
	}
	s.codeModel = modelName
}

func (s *Service) SetCodeIndex(index model.Index) {
	if index == nil {
		return
	}
	s.codeIndex = index
}

func (s *Service) SetChunkMetadata(label uint64, metadata model.SearchHit) {
	s.metaMu.Lock()
	s.chunkByLabel[label] = metadata
	s.metaMu.Unlock()
}

func (s *Service) Search(ctx context.Context, query model.SearchQuery) ([]model.SearchHit, error) {
	_ = s.store

	k := query.K
	if k <= 0 {
		k = 10
	}

	mode := strings.ToLower(strings.TrimSpace(query.Index))
	if mode == "" {
		mode = "auto"
	}
	switch mode {
	case "text":
		return s.searchSingleIndex(ctx, query.Query, k, s.textModel, s.textIndex, query)
	case "code":
		return s.searchSingleIndex(ctx, query.Query, k, s.codeModel, s.codeIndex, query)
	case "both":
		return s.searchBothIndices(ctx, query.Query, k, query)
	case "auto":
		if looksLikeCodeQuery(query.Query) {
			return s.searchSingleIndex(ctx, query.Query, k, s.codeModel, s.codeIndex, query)
		}
		return s.searchSingleIndex(ctx, query.Query, k, s.textModel, s.textIndex, query)
	default:
		return s.searchSingleIndex(ctx, query.Query, k, s.textModel, s.textIndex, query)
	}
}

func (s *Service) Ask(ctx context.Context, question string, query model.SearchQuery) (model.AskResult, error) {
	_ = ctx
	_ = question
	_ = query
	return model.AskResult{}, model.ErrNotImplemented
}

func (s *Service) OpenFile(ctx context.Context, relPath string, span model.Span, maxChars int) (string, error) {
	_ = ctx
	_ = relPath
	_ = span
	_ = maxChars
	return "", model.ErrNotImplemented
}

func (s *Service) Stats(ctx context.Context) (model.Stats, error) {
	_ = ctx
	return model.Stats{}, model.ErrNotImplemented
}

func (s *Service) searchHitForLabel(label uint64) model.SearchHit {
	s.metaMu.RLock()
	meta, ok := s.chunkByLabel[label]
	s.metaMu.RUnlock()

	if ok {
		meta.ChunkID = int64(label)
		return meta
	}

	return model.SearchHit{
		ChunkID: int64(label),
		RelPath: "",
		DocType: "unknown",
		RepType: "unknown",
		Snippet: "",
		Span:    model.Span{Kind: "lines"},
	}
}

func (s *Service) searchSingleIndex(ctx context.Context, query string, k int, modelName string, idx model.Index, filters model.SearchQuery) ([]model.SearchHit, error) {
	vectors, err := s.embedder.Embed(ctx, modelName, []string{query})
	if err != nil {
		return nil, err
	}
	if len(vectors) == 0 {
		return []model.SearchHit{}, nil
	}
	if idx == nil {
		return []model.SearchHit{}, nil
	}

	labels, scores, err := idx.Search(vectors[0], k*5)
	if err != nil {
		return nil, err
	}

	filtered := make([]model.SearchHit, 0, k)
	for i, label := range labels {
		hit := s.searchHitForLabel(label)
		hit.Score = float64(scores[i])
		if !matchFilters(hit, filters) {
			continue
		}
		filtered = append(filtered, hit)
		if len(filtered) >= k {
			break
		}
	}
	return filtered, nil
}

func (s *Service) searchBothIndices(ctx context.Context, query string, k int, filters model.SearchQuery) ([]model.SearchHit, error) {
	textHits, err := s.searchSingleIndex(ctx, query, k*5, s.textModel, s.textIndex, filters)
	if err != nil {
		return nil, err
	}
	codeHits, err := s.searchSingleIndex(ctx, query, k*5, s.codeModel, s.codeIndex, filters)
	if err != nil {
		return nil, err
	}

	normalizeScores(textHits)
	normalizeScores(codeHits)

	merged := make(map[int64]model.SearchHit)
	for _, hit := range textHits {
		merged[hit.ChunkID] = hit
	}
	for _, hit := range codeHits {
		if existing, ok := merged[hit.ChunkID]; ok {
			if hit.Score > existing.Score {
				merged[hit.ChunkID] = hit
			}
			continue
		}
		merged[hit.ChunkID] = hit
	}

	out := make([]model.SearchHit, 0, len(merged))
	for _, hit := range merged {
		out = append(out, hit)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Score == out[j].Score {
			return out[i].ChunkID < out[j].ChunkID
		}
		return out[i].Score > out[j].Score
	})
	if len(out) > k {
		out = out[:k]
	}
	return out, nil
}

func normalizeScores(hits []model.SearchHit) {
	if len(hits) == 0 {
		return
	}

	minScore := math.Inf(1)
	maxScore := math.Inf(-1)
	for _, hit := range hits {
		if hit.Score < minScore {
			minScore = hit.Score
		}
		if hit.Score > maxScore {
			maxScore = hit.Score
		}
	}
	if maxScore == minScore {
		for i := range hits {
			hits[i].Score = 1
		}
		return
	}

	denom := maxScore - minScore
	for i := range hits {
		hits[i].Score = (hits[i].Score - minScore) / denom
	}
}

func looksLikeCodeQuery(query string) bool {
	q := strings.ToLower(query)
	codeTokens := []string{"func ", "class ", "package ", "import ", "return ", "if (", "{", "}", "[]", "go ", "python", "typescript", "java"}
	for _, token := range codeTokens {
		if strings.Contains(q, token) {
			return true
		}
	}
	return false
}

func matchFilters(hit model.SearchHit, query model.SearchQuery) bool {
	if query.PathPrefix != "" && !strings.HasPrefix(hit.RelPath, query.PathPrefix) {
		return false
	}

	if query.FileGlob != "" {
		matched, err := path.Match(query.FileGlob, hit.RelPath)
		if err != nil || !matched {
			return false
		}
	}

	if len(query.DocTypes) > 0 {
		docTypeMatch := false
		for _, docType := range query.DocTypes {
			if strings.EqualFold(strings.TrimSpace(docType), strings.TrimSpace(hit.DocType)) {
				docTypeMatch = true
				break
			}
		}
		if !docTypeMatch {
			return false
		}
	}

	return true
}
