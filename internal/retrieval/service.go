package retrieval

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"

	"dir2mcp/internal/model"
)

var (
	// compiled regexes used by looksLikeCodeQuery; moved out of the
	// function to avoid rebuilding on every invocation.
	codeKeywordRe   = regexp.MustCompile(`\b(func|class|package|import|return|if|for|while|switch|case)\b`)
	codePunctRe     = regexp.MustCompile(`[(){}\[\];]`)
	fileExtensionRe = regexp.MustCompile(`\.(js|ts|py|go|java|rb|cpp|c|cs|html|css|json|yaml|yml)\b`)
	timePrefixRe    = regexp.MustCompile(`^\s*\[?(\d{1,2}):(\d{2})(?::(\d{2}))?\]?\s*(.*)$`)
)

var defaultPathExcludes = []string{
	"**/.git/**",
	"**/node_modules/**",
	"**/.dir2mcp/**",
	"**/.env",
	"**/*.pem",
	"**/*.key",
	"**/id_rsa",
}

var defaultSecretPatternLiterals = []string{
	`AKIA[0-9A-Z]{16}`,
	`(?i)(?:aws(?:.{0,20})?secret|(?:secret|aws|token|key)\s*[:=]\s*[0-9a-zA-Z/+=]{40})`,

	`(?i)(?:authorization\s*[:=]\s*bearer\s+|(?:access|id|refresh)_token\s*[:=]\s*)[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}`,
	`(?i)token\s*[:=]\s*[A-Za-z0-9_.-]{20,}`,
	`sk_[a-z0-9]{32}|api_[A-Za-z0-9]{32}`,
}

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
	rootDir             string
	pathExcludes        []string
	// cached compiled regexps for exclude patterns; keys are normalized patterns
	excludeRegexps map[string]*regexp.Regexp
	secretPatterns []*regexp.Regexp
}

func NewService(store model.Store, index model.Index, embedder model.Embedder, gen model.Generator) *Service {
	compiledPatterns := make([]*regexp.Regexp, 0, len(defaultSecretPatternLiterals))
	for _, pattern := range defaultSecretPatternLiterals {
		re, err := regexp.Compile(pattern)
		if err != nil {
			panic(fmt.Errorf("invalid default secret pattern %q: %w", pattern, err))
		}
		compiledPatterns = append(compiledPatterns, re)
	}
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
		rootDir:        ".",
		excludeRegexps: make(map[string]*regexp.Regexp),
		pathExcludes:   append([]string(nil), defaultPathExcludes...),
		secretPatterns: compiledPatterns,
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

func (s *Service) SetRootDir(root string) {
	root = strings.TrimSpace(root)
	if root == "" {
		root = "."
	}
	s.metaMu.Lock()
	s.rootDir = root
	s.metaMu.Unlock()
}

func (s *Service) SetPathExcludes(patterns []string) {
	copied := append([]string(nil), patterns...)
	compiled := make(map[string]*regexp.Regexp, len(copied))
	for _, pat := range copied {
		norm := strings.TrimSpace(filepath.ToSlash(pat))
		if norm == "" {
			continue
		}
		re, err := regexp.Compile(globToRegexp(norm))
		if err != nil {
			// ignore invalid pattern, it'll simply never match
			continue
		}
		compiled[norm] = re
	}

	s.metaMu.Lock()
	// store normalized list so callers reading pathExcludes see the same
	// values used as keys in the regexp cache.
	s.pathExcludes = copied
	s.excludeRegexps = compiled
	s.metaMu.Unlock()
}

func (s *Service) SetSecretPatterns(patterns []string) error {
	compiled := make([]*regexp.Regexp, 0, len(patterns))
	for _, pattern := range patterns {
		re, err := regexp.Compile(pattern)
		if err != nil {
			return err
		}
		compiled = append(compiled, re)
	}
	s.metaMu.Lock()
	s.secretPatterns = compiled
	s.metaMu.Unlock()
	return nil
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
	content, _, err := s.openFile(ctx, relPath, span, maxChars)
	return content, err
}

func (s *Service) OpenFileWithMeta(ctx context.Context, relPath string, span model.Span, maxChars int) (string, bool, error) {
	return s.openFile(ctx, relPath, span, maxChars)
}

func (s *Service) openFile(ctx context.Context, relPath string, span model.Span, maxChars int) (string, bool, error) {
	if err := ctx.Err(); err != nil {
		return "", false, err
	}
	relPath = strings.TrimSpace(relPath)
	if relPath == "" {
		return "", false, model.ErrForbidden
	}

	if maxChars <= 0 {
		maxChars = 20000
	}
	if maxChars > 50000 {
		maxChars = 50000
	}

	s.metaMu.RLock()
	rootDir := s.rootDir
	pathExcludes := append([]string(nil), s.pathExcludes...)
	secretPatterns := append([]*regexp.Regexp(nil), s.secretPatterns...)
	s.metaMu.RUnlock()
	if strings.TrimSpace(rootDir) == "" {
		rootDir = "."
	}

	normalizedRel := filepath.ToSlash(filepath.Clean(relPath))
	if normalizedRel == "." || strings.HasPrefix(normalizedRel, "../") || normalizedRel == ".." || filepath.IsAbs(relPath) {
		return "", false, model.ErrPathOutsideRoot
	}
	for _, pattern := range pathExcludes {
		if s.matchExcludePattern(pattern, normalizedRel) {
			return "", false, model.ErrForbidden
		}
	}

	rootAbs, err := filepath.Abs(rootDir)
	if err != nil {
		return "", false, err
	}
	realRoot := rootAbs
	if resolvedRoot, rootErr := filepath.EvalSymlinks(rootAbs); rootErr == nil {
		realRoot = resolvedRoot
	}

	targetAbs := filepath.Join(realRoot, filepath.FromSlash(normalizedRel))
	relFromRoot, err := filepath.Rel(realRoot, targetAbs)
	if err != nil || relFromRoot == ".." || strings.HasPrefix(relFromRoot, ".."+string(os.PathSeparator)) {
		return "", false, model.ErrPathOutsideRoot
	}

	kind := strings.ToLower(strings.TrimSpace(span.Kind))
	if kind == "page" || kind == "time" {
		if fromMeta, ok := s.sliceFromMetadata(normalizedRel, span); ok {
			for _, re := range secretPatterns {
				if re != nil && re.MatchString(fromMeta) {
					return "", false, model.ErrForbidden
				}
			}
			out, truncated := truncateRunesWithFlag(fromMeta, maxChars)
			return out, truncated, nil
		}
	}

	resolvedAbs, err := filepath.EvalSymlinks(targetAbs)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", false, err
		}
		// if eval fails for other reasons, continue with direct target path check
		resolvedAbs = targetAbs
	}
	resolvedRel, err := filepath.Rel(realRoot, resolvedAbs)
	if err != nil || resolvedRel == ".." || strings.HasPrefix(resolvedRel, ".."+string(os.PathSeparator)) {
		return "", false, model.ErrPathOutsideRoot
	}
	resolvedRel = filepath.ToSlash(filepath.Clean(resolvedRel))
	for _, pattern := range pathExcludes {
		if s.matchExcludePattern(pattern, resolvedRel) {
			return "", false, model.ErrForbidden
		}
	}

	info, err := os.Stat(resolvedAbs)
	if err != nil {
		return "", false, err
	}
	if info.IsDir() {
		return "", false, model.ErrDocTypeUnsupported
	}

	raw, readTruncated, err := readFileBounded(resolvedAbs, maxChars+1)
	if err != nil {
		return "", false, err
	}
	content := string(raw)

	for _, re := range secretPatterns {
		if re != nil && re.MatchString(content) {
			return "", false, model.ErrForbidden
		}
	}

	selected := content
	switch kind {
	case "", "lines":
		if kind == "lines" || span.StartLine > 0 || span.EndLine > 0 {
			selected = sliceLines(content, span.StartLine, span.EndLine)
		}
	case "page":
		page := span.Page
		if page <= 0 {
			page = 1
		}
		// metadata-backed OCR handled above; fall back to slicing pages directly
		paged, ok := slicePage(content, page)
		if !ok {
			return "", false, model.ErrDocTypeUnsupported
		}
		selected = paged
	case "time":
		startMS := span.StartMS
		endMS := span.EndMS
		if startMS < 0 {
			startMS = 0
		}
		if endMS < 0 {
			endMS = 0
		}
		if endMS > 0 && endMS < startMS {
			endMS = startMS
		}
		// metadata-backed slices for time spans are handled earlier; just extract
		timeSlice, ok := sliceTime(content, startMS, endMS)
		if !ok {
			return "", false, model.ErrDocTypeUnsupported
		}
		selected = timeSlice
	default:
		return "", false, model.ErrDocTypeUnsupported
	}

	out, outTruncated := truncateRunesWithFlag(selected, maxChars)
	return out, readTruncated || outTruncated, nil
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

// ErrMissingEmbedder is returned when the service was created without
// a configured embedder and a search attempt is made. This prevents a
// nil-pointer panic in searchSingleIndex while giving callers a clear
// failure reason.
var ErrMissingEmbedder = errors.New("embedder not configured")

func (s *Service) searchSingleIndex(ctx context.Context, query string, k int, modelName string, idx model.Index, indexName string, filters model.SearchQuery) ([]model.SearchHit, error) {
	if s.embedder == nil {
		// caller should have provided an embedder via NewService or
		// SetEmbedder (not currently available).  Return an explicit
		// error rather than letting the nil dereference panic later.
		return nil, ErrMissingEmbedder
	}
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
	// protect multiplication k * overfetchMultiplier against overflow
	// by checking against MaxInt. If the caller supplied a huge k value we
	// simply clamp the request size rather than allow wraparound.  An
	// alternative would be to return an error; at present callers only ever
	// pass reasonably small k's so clamping is acceptable.
	var n int
	if k > math.MaxInt/overfetchMultiplier {
		// avoid overflow and also prevent asking the index for more
		// neighbors than an int can represent; this keeps downstream code
		// consistent (e.g. fakeIndex in tests) and mirrors the behavior of
		// capping the multiplier itself via SetOverfetchMultiplier.
		n = math.MaxInt
	} else {
		n = k * overfetchMultiplier
	}
	labels, scores, err := idx.Search(vectors[0], n)
	if err != nil {
		return nil, err
	}

	// avoid trying to preallocate a gigantic slice when k is absurdly large
	cap := k
	if cap > len(labels) {
		cap = len(labels)
	}
	filtered := make([]model.SearchHit, 0, cap)
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

func (s *Service) matchExcludePattern(pattern, relPath string) bool {
	pattern = strings.TrimSpace(filepath.ToSlash(pattern))
	relPath = strings.TrimSpace(filepath.ToSlash(relPath))
	if pattern == "" || relPath == "" {
		return false
	}

	// look up precompiled regexp
	s.metaMu.RLock()
	re := s.excludeRegexps[pattern]
	s.metaMu.RUnlock()
	if re == nil {
		// compile lazily in case cache was missed; store for future
		var err error
		re, err = regexp.Compile(globToRegexp(pattern))
		if err != nil {
			return false
		}
		s.metaMu.Lock()
		if s.excludeRegexps == nil {
			s.excludeRegexps = make(map[string]*regexp.Regexp)
		}
		s.excludeRegexps[pattern] = re
		s.metaMu.Unlock()
	}
	return re.MatchString(relPath)
}

func globToRegexp(glob string) string {
	var b strings.Builder
	b.WriteString("^")
	for i := 0; i < len(glob); {
		c := glob[i]
		switch c {
		case '*':
			if i+1 < len(glob) && glob[i+1] == '*' {
				i += 2
				if i < len(glob) && glob[i] == '/' {
					i++
					b.WriteString("(?:.*/)?")
				} else {
					b.WriteString(".*")
				}
				continue
			}
			b.WriteString(`[^/]*`)
		case '?':
			b.WriteString(`[^/]`)
		default:
			if strings.ContainsRune(`.+()|[]{}^$\`, rune(c)) {
				b.WriteByte('\\')
			}
			b.WriteByte(c)
		}
		i++
	}
	b.WriteString("$")
	return b.String()
}

func sliceLines(content string, start, end int) string {
	lines := strings.Split(content, "\n")
	if start <= 0 {
		start = 1
	}
	if end <= 0 {
		end = start
	}
	if start > len(lines) {
		return ""
	}
	if end > len(lines) {
		end = len(lines)
	}
	if end < start {
		end = start
	}
	return strings.Join(lines[start-1:end], "\n")
}

func truncateRunesWithFlag(s string, max int) (string, bool) {
	if max <= 0 {
		return s, false
	}
	r := []rune(s)
	if len(r) <= max {
		return s, false
	}
	return string(r[:max]), true
}

func readFileBounded(path string, maxBytes int) ([]byte, bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, false, err
	}
	defer func() { _ = f.Close() }()

	if maxBytes <= 0 {
		data, readErr := io.ReadAll(f)
		return data, false, readErr
	}

	lim := io.LimitReader(f, int64(maxBytes))
	data, readErr := io.ReadAll(lim)
	if readErr != nil {
		return nil, false, readErr
	}
	return data, len(data) == maxBytes, nil
}

func (s *Service) sliceFromMetadata(relPath string, requested model.Span) (string, bool) {
	s.metaMu.RLock()
	defer s.metaMu.RUnlock()

	type candidate struct {
		start int
		page  int
		text  string
	}
	matches := make([]candidate, 0, 8)

	for _, hit := range s.chunkByLabel {
		if strings.TrimSpace(filepath.ToSlash(hit.RelPath)) != strings.TrimSpace(filepath.ToSlash(relPath)) {
			continue
		}
		if strings.TrimSpace(hit.Snippet) == "" {
			continue
		}
		span := hit.Span
		switch requested.Kind {
		case "page":
			if strings.EqualFold(span.Kind, "page") && span.Page == requested.Page {
				matches = append(matches, candidate{page: span.Page, text: hit.Snippet})
			}
		case "time":
			if !strings.EqualFold(span.Kind, "time") {
				continue
			}
			if overlapsTime(span.StartMS, span.EndMS, requested.StartMS, requested.EndMS) {
				matches = append(matches, candidate{start: span.StartMS, text: hit.Snippet})
			}
		}
	}

	if len(matches) == 0 {
		return "", false
	}
	sort.Slice(matches, func(i, j int) bool {
		if matches[i].page != matches[j].page {
			return matches[i].page < matches[j].page
		}
		return matches[i].start < matches[j].start
	})

	out := make([]string, 0, len(matches))
	for _, m := range matches {
		out = append(out, m.text)
	}
	return strings.Join(out, "\n"), true
}

func overlapsTime(aStart, aEnd, bStart, bEnd int) bool {
	if aEnd <= 0 {
		aEnd = aStart
	}
	if bEnd <= 0 {
		bEnd = bStart
	}
	if aEnd < aStart {
		aEnd = aStart
	}
	if bEnd < bStart {
		bEnd = bStart
	}
	return aStart <= bEnd && bStart <= aEnd
}

func slicePage(content string, page int) (string, bool) {
	if page <= 0 {
		page = 1
	}
	parts := strings.Split(content, "\f")
	if len(parts) > 1 {
		if page > len(parts) {
			return "", false
		}
		return strings.Trim(parts[page-1], "\n"), true
	}
	if page == 1 {
		return content, true
	}
	return "", false
}

func sliceTime(content string, startMS, endMS int) (string, bool) {
	lines := strings.Split(content, "\n")
	out := make([]string, 0, len(lines))
	foundTimestamp := false

	for _, line := range lines {
		m := timePrefixRe.FindStringSubmatch(line)
		if len(m) == 0 {
			continue
		}
		foundTimestamp = true
		tsMS := parseTimestampMS(m[1], m[2], m[3])
		if tsMS < startMS {
			continue
		}
		if endMS > 0 && tsMS > endMS {
			continue
		}
		out = append(out, line)
	}

	if !foundTimestamp {
		return "", false
	}
	if len(out) == 0 {
		return "", true
	}
	return strings.Join(out, "\n"), true
}

func parseTimestampMS(a, b, c string) int {
	x, _ := strconv.Atoi(a)
	y, _ := strconv.Atoi(b)
	if c == "" {
		return (x*60 + y) * 1000
	}
	z, _ := strconv.Atoi(c)
	return (x*3600 + y*60 + z) * 1000
}
