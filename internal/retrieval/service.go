package retrieval

import (
	"context"
	"math"
	"path"
	"regexp"
	"sort"
	"strings"
	"sync"

	"github.com/Dirstral/dir2mcp/internal/model"
)

var (
	// compiled regexes used by looksLikeCodeQuery; moved out of the
	// function to avoid rebuilding on every invocation.
	codeKeywordRe   = regexp.MustCompile(`\b(func|class|package|import|return|if|for|while|switch|case)\b`)
	codePunctRe     = regexp.MustCompile(`[(){}\[\];]`)
	fileExtensionRe = regexp.MustCompile(`\.(js|ts|py|go|java|rb|cpp|c|cs|html|css|json|yaml|yml)\b`)
)

// Service implements retrieval operations over embedded data.
// It holds necessary components like store, index, embedder and
// supports configurable overfetching during searches. OverfetchMultiplier
// determines how many candidates the underlying index returns per
// requested hit. A multiplier of 1 means no overfetch; the default is 5
// which generally provides enough buffer for downstream filtering.
// Callers may tune the value via SetOverfetchMultiplier, and it is validated
// to be at least 1 (higher values are capped at 100 to avoid runaway work).
//
// NOTE: adjusting the multiplier can help when heavy filtering is applied
// or when `k` is large; a smaller value reduces work at the cost of
// potentially missing some matches.
//
// WARNING: changing this value after the service has been used may
// affect the semantics of subsequent searches.
//
// The field is unexported to encourage use of the setter where
// validation takes place.
//
// See NewService for default initialization details.

type Service struct {
	store               model.Store
	textIndex           model.Index
	codeIndex           model.Index
	embedder            model.Embedder
	gen                 model.Generator
	textModel           string
	codeModel           string
	overfetchMultiplier int
	metaMu              sync.RWMutex
	chunkByLabel        map[uint64]model.SearchHit
	chunkByIndex        map[string]map[uint64]model.SearchHit
}

func NewService(store model.Store, index model.Index, embedder model.Embedder, gen model.Generator) *Service {
	// overfetchMultiplier defaults to 5; callers may override it with
	// SetOverfetchMultiplier to tune for their workload.  Values less than
	// 1 are silently bumped to 1, and values above 100 are capped.
	return &Service{
		store:               store,
		textIndex:           index,
		codeIndex:           index,
		embedder:            embedder,
		gen:                 gen,
		textModel:           "mistral-embed",
		codeModel:           "codestral-embed",
		overfetchMultiplier: 5,
		chunkByLabel:        make(map[uint64]model.SearchHit),
		chunkByIndex: map[string]map[uint64]model.SearchHit{
			"text": make(map[uint64]model.SearchHit),
			"code": make(map[uint64]model.SearchHit),
		},
	}
}

func (s *Service) SetQueryEmbeddingModel(modelName string) {
	if strings.TrimSpace(modelName) == "" {
		return
	}
	s.metaMu.Lock()
	s.textModel = modelName
	s.metaMu.Unlock()
}

func (s *Service) SetCodeEmbeddingModel(modelName string) {
	if strings.TrimSpace(modelName) == "" {
		return
	}
	s.metaMu.Lock()
	s.codeModel = modelName
	s.metaMu.Unlock()
}

func (s *Service) SetCodeIndex(index model.Index) {
	if index == nil {
		return
	}
	s.metaMu.Lock()
	s.codeIndex = index
	s.metaMu.Unlock()
}

func (s *Service) SetChunkMetadata(label uint64, metadata model.SearchHit) {
	s.metaMu.Lock()
	s.chunkByLabel[label] = metadata
	s.chunkByIndex["text"][label] = metadata
	s.chunkByIndex["code"][label] = metadata
	s.metaMu.Unlock()
}

func (s *Service) SetChunkMetadataForIndex(indexName string, label uint64, metadata model.SearchHit) {
	kind := strings.ToLower(strings.TrimSpace(indexName))
	if kind != "text" && kind != "code" {
		s.SetChunkMetadata(label, metadata)
		return
	}

	s.metaMu.Lock()
	s.chunkByLabel[label] = metadata
	s.chunkByIndex[kind][label] = metadata
	s.metaMu.Unlock()
}

// SetOverfetchMultiplier changes the multiplier used when querying the
// underlying vector index.  The service will ask for `k * multiplier`
// neighbors for a request that originally asked for `k` hits.  Values
// lower than 1 are bumped to 1 (no overfetch) and values greater than
// 100 are capped to prevent unreasonable work.  This method is safe to
// call concurrently.
func (s *Service) SetOverfetchMultiplier(m int) {
	if m < 1 {
		m = 1
	}
	if m > 100 {
		m = 100
	}
	s.metaMu.Lock()
	s.overfetchMultiplier = m
	s.metaMu.Unlock()
}

func (s *Service) Search(ctx context.Context, query model.SearchQuery) ([]model.SearchHit, error) {
	s.metaMu.RLock()
	textModel := s.textModel
	codeModel := s.codeModel
	textIndex := s.textIndex
	codeIndex := s.codeIndex
	s.metaMu.RUnlock()

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
		return s.searchSingleIndex(ctx, query.Query, k, textModel, textIndex, "text", query)
	case "code":
		return s.searchSingleIndex(ctx, query.Query, k, codeModel, codeIndex, "code", query)
	case "both":
		return s.searchBothIndices(ctx, query.Query, k, textModel, codeModel, textIndex, codeIndex, query)
	case "auto":
		if looksLikeCodeQuery(query.Query) {
			return s.searchSingleIndex(ctx, query.Query, k, codeModel, codeIndex, "code", query)
		}
		return s.searchSingleIndex(ctx, query.Query, k, textModel, textIndex, "text", query)
	default:
		return s.searchSingleIndex(ctx, query.Query, k, textModel, textIndex, "text", query)
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

func (s *Service) searchHitForLabel(indexName string, label uint64) model.SearchHit {
	s.metaMu.RLock()
	if byIndex, ok := s.chunkByIndex[indexName]; ok {
		if meta, exists := byIndex[label]; exists {
			s.metaMu.RUnlock()
			meta.ChunkID = int64(label)
			return meta
		}
	}
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

func (s *Service) searchSingleIndex(ctx context.Context, query string, k int, modelName string, idx model.Index, indexName string, filters model.SearchQuery) ([]model.SearchHit, error) {
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

	// compute number of neighbors to request from index according to the
	// current overfetch multiplier. Read under lock to avoid races with
	// SetOverfetchMultiplier. The multiplier is initialized to 5 in the
	// constructor and SetOverfetchMultiplier already clamps values to the
	// [1,100] range, so it is guaranteed to be at least 1 and no further
	// defensive adjustment is necessary.
	s.metaMu.RLock()
	overfetchMultiplier := s.overfetchMultiplier
	s.metaMu.RUnlock()
	n := k * overfetchMultiplier
	labels, scores, err := idx.Search(vectors[0], n)
	if err != nil {
		return nil, err
	}

	filtered := make([]model.SearchHit, 0, k)
	for i, label := range labels {
		hit := s.searchHitForLabel(indexName, label)
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

func (s *Service) searchBothIndices(ctx context.Context, query string, k int, textModel, codeModel string, textIndex, codeIndex model.Index, filters model.SearchQuery) ([]model.SearchHit, error) {
	// each single-index call will apply the overfetch multiplier internally
	textHits, err := s.searchSingleIndex(ctx, query, k, textModel, textIndex, "text", filters)
	if err != nil {
		return nil, err
	}
	codeHits, err := s.searchSingleIndex(ctx, query, k, codeModel, codeIndex, "code", filters)
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

	// keyword pattern with word boundaries to avoid matching substrings.
	hasKw := codeKeywordRe.MatchString(q)
	// punctuation tokens commonly found in code
	hasPunct := codePunctRe.MatchString(q)
	// fenced code blocks or backticks
	hasFenced := strings.Contains(q, "```")
	hasBacktick := strings.Contains(q, "`")
	// file extension-like indicator – restrict to common code extensions and ensure a word boundary
	hasFileExt := fileExtensionRe.MatchString(q)

	// a strong signal: keyword + punctuation nearby
	if hasKw && hasPunct {
		return true
	}

	// otherwise count independent indicators
	indicators := 0
	if hasKw {
		indicators++
	}
	if hasPunct {
		indicators++
	}
	if hasFenced {
		indicators++
	}
	if hasBacktick {
		indicators++
	}
	if hasFileExt {
		indicators++
	}
	return indicators >= 2
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
