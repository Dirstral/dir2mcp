package mcp

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"dir2mcp/internal/mistral"
	"dir2mcp/internal/model"
)

const (
	toolNameSearch    = "dir2mcp.search"
	toolNameAsk       = "dir2mcp.ask"
	toolNameAskAudio  = "dir2mcp.ask_audio"
	toolNameOpenFile  = "dir2mcp.open_file"
	toolNameListFiles = "dir2mcp.list_files"
	toolNameStats     = "dir2mcp.stats"

	defaultEmbedTextModel = "mistral-embed"
	defaultEmbedCodeModel = "codestral-embed"
	defaultOCRModel       = mistral.DefaultOCRModel
	defaultSTTProvider    = "mistral"
	defaultSTTModel       = "voxtral-mini-latest"
	defaultChatModel      = "mistral-small-2506"
)

var toolOrder = []string{
	toolNameSearch,
	toolNameAsk,
	toolNameAskAudio,
	toolNameOpenFile,
	toolNameListFiles,
	toolNameStats,
}

type toolHandler func(context.Context, map[string]interface{}) (toolCallResult, *toolExecutionError)

type toolDefinition struct {
	Name         string                 `json:"name"`
	Description  string                 `json:"description"`
	InputSchema  map[string]interface{} `json:"inputSchema"`
	OutputSchema map[string]interface{} `json:"outputSchema,omitempty"`
	handler      toolHandler            `json:"-"`
}

type toolsCallParams struct {
	Name      string                 `json:"name"`
	Arguments map[string]interface{} `json:"arguments,omitempty"`
}

type toolCallResult struct {
	Content           []toolContentItem `json:"content"`
	StructuredContent interface{}       `json:"structuredContent,omitempty"`
	IsError           bool              `json:"isError,omitempty"`
}

type toolContentItem struct {
	Type     string `json:"type"`
	Text     string `json:"text,omitempty"`
	Data     string `json:"data,omitempty"`
	MIMEType string `json:"mimeType,omitempty"`
}

type toolExecutionError struct {
	Code      string
	Message   string
	Retryable bool
}

type retrieverOpenFileWithMeta interface {
	OpenFileWithMeta(ctx context.Context, relPath string, span model.Span, maxChars int) (string, bool, error)
}

type voiceAwareTTSSynthesizer interface {
	SynthesizeWithVoice(ctx context.Context, text, voiceID string) ([]byte, error)
}

func (s *Server) buildToolRegistry() map[string]toolDefinition {
	return map[string]toolDefinition{
		toolNameSearch: {
			Name:         toolNameSearch,
			Description:  "Semantic retrieval across indexed content.",
			InputSchema:  searchInputSchema(),
			OutputSchema: searchOutputSchema(),
			handler:      s.handleSearchTool,
		},
		toolNameAsk: {
			Name:         toolNameAsk,
			Description:  "RAG answer with citations; can run search-only mode.",
			InputSchema:  askInputSchema(),
			OutputSchema: askOutputSchema(),
			handler:      s.handleAskTool,
		},
		toolNameAskAudio: {
			Name:         toolNameAskAudio,
			Description:  "RAG answer with optional ElevenLabs audio synthesis.",
			InputSchema:  askAudioInputSchema(),
			OutputSchema: askAudioOutputSchema(),
			handler:      s.handleAskAudioTool,
		},
		toolNameOpenFile: {
			Name:         toolNameOpenFile,
			Description:  "Open an exact source slice for verification.",
			InputSchema:  openFileInputSchema(),
			OutputSchema: openFileOutputSchema(),
			handler:      s.handleOpenFileTool,
		},
		toolNameListFiles: {
			Name:         toolNameListFiles,
			Description:  "List files under root for navigation and filter selection.",
			InputSchema:  listFilesInputSchema(),
			OutputSchema: listFilesOutputSchema(),
			handler:      s.handleListFilesTool,
		},
		toolNameStats: {
			Name:         toolNameStats,
			Description:  "Status/progress/health for indexing and models.",
			InputSchema:  statsInputSchema(),
			OutputSchema: statsOutputSchema(),
			handler:      s.handleStatsTool,
		},
	}
}

func (s *Server) handleToolsList(w http.ResponseWriter, id interface{}) {
	tools := make([]toolDefinition, 0, len(s.tools))

	for _, name := range toolOrder {
		if tool, ok := s.tools[name]; ok {
			tools = append(tools, tool)
		}
	}

	if len(tools) == 0 {
		names := make([]string, 0, len(s.tools))
		for name := range s.tools {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			tools = append(tools, s.tools[name])
		}
	}

	writeResult(w, http.StatusOK, id, map[string]interface{}{
		"tools": tools,
	})
}

func (s *Server) handleToolsCall(ctx context.Context, w http.ResponseWriter, rawParams json.RawMessage, id interface{}) {
	params, err := parseToolsCallParams(rawParams)
	if err != nil {
		canonicalCode := "INVALID_FIELD"
		var vErr validationError
		if errors.As(err, &vErr) && vErr.canonicalCode != "" {
			canonicalCode = vErr.canonicalCode
		}
		writeError(w, http.StatusBadRequest, id, -32600, err.Error(), canonicalCode, false)
		return
	}

	tool, ok := s.tools[params.Name]
	if !ok {
		writeResult(w, http.StatusOK, id, newToolErrorResult(toolExecutionError{
			Code:      "METHOD_NOT_FOUND",
			Message:   fmt.Sprintf("unknown tool: %s", params.Name),
			Retryable: false,
		}))
		return
	}

	result, toolErr := tool.handler(ctx, params.Arguments)
	if toolErr != nil {
		writeResult(w, http.StatusOK, id, newToolErrorResult(*toolErr))
		return
	}

	writeResult(w, http.StatusOK, id, result)
}

func parseToolsCallParams(raw json.RawMessage) (toolsCallParams, error) {
	if len(raw) == 0 {
		return toolsCallParams{}, validationError{
			message:       "params is required",
			canonicalCode: "MISSING_FIELD",
		}
	}

	var params toolsCallParams
	if err := json.Unmarshal(raw, &params); err != nil {
		return toolsCallParams{}, validationError{
			message:       "invalid tools/call params",
			canonicalCode: "INVALID_FIELD",
		}
	}

	params.Name = strings.TrimSpace(params.Name)
	if params.Name == "" {
		return toolsCallParams{}, validationError{
			message:       "tools/call params.name is required",
			canonicalCode: "MISSING_FIELD",
		}
	}
	if params.Arguments == nil {
		params.Arguments = map[string]interface{}{}
	}

	return params, nil
}

func newToolErrorResult(toolErr toolExecutionError) toolCallResult {
	text := fmt.Sprintf("ERROR: %s: %s", toolErr.Code, toolErr.Message)
	return toolCallResult{
		IsError: true,
		Content: []toolContentItem{
			{Type: "text", Text: text},
		},
		StructuredContent: map[string]interface{}{
			"error": map[string]interface{}{
				"code":      toolErr.Code,
				"message":   toolErr.Message,
				"retryable": toolErr.Retryable,
			},
		},
	}
}

func (s *Server) handleStatsTool(_ context.Context, args map[string]interface{}) (toolCallResult, *toolExecutionError) {
	if err := assertNoUnknownArguments(args, map[string]struct{}{}); err != nil {
		return toolCallResult{}, &toolExecutionError{Code: "INVALID_FIELD", Message: err.Error(), Retryable: false}
	}

	snapshot := s.indexing.Snapshot()
	structured := map[string]interface{}{
		"root":             s.cfg.RootDir,
		"state_dir":        s.cfg.StateDir,
		"protocol_version": s.cfg.ProtocolVersion,
		"indexing": map[string]interface{}{
			"job_id":          snapshot.JobID,
			"running":         snapshot.Running,
			"mode":            snapshot.Mode,
			"scanned":         snapshot.Scanned,
			"indexed":         snapshot.Indexed,
			"skipped":         snapshot.Skipped,
			"deleted":         snapshot.Deleted,
			"representations": snapshot.Representations,
			"chunks_total":    snapshot.ChunksTotal,
			"embedded_ok":     snapshot.EmbeddedOK,
			"errors":          snapshot.Errors,
		},
		"models": map[string]interface{}{
			"embed_text":   defaultEmbedTextModel,
			"embed_code":   defaultEmbedCodeModel,
			"ocr":          defaultOCRModel,
			"stt_provider": defaultSTTProvider,
			"stt_model":    defaultSTTModel,
			"chat":         defaultChatModel,
		},
	}

	text := fmt.Sprintf(
		"indexing running=%t scanned=%d indexed=%d errors=%d",
		snapshot.Running,
		snapshot.Scanned,
		snapshot.Indexed,
		snapshot.Errors,
	)

	return toolCallResult{
		Content: []toolContentItem{
			{Type: "text", Text: text},
		},
		StructuredContent: structured,
	}, nil
}

func (s *Server) handleListFilesTool(ctx context.Context, args map[string]interface{}) (toolCallResult, *toolExecutionError) {
	if err := assertNoUnknownArguments(args, map[string]struct{}{
		"path_prefix": {},
		"glob":        {},
		"limit":       {},
		"offset":      {},
	}); err != nil {
		return toolCallResult{}, &toolExecutionError{Code: "INVALID_FIELD", Message: err.Error(), Retryable: false}
	}

	pathPrefix, err := parseOptionalString(args, "path_prefix")
	if err != nil {
		return toolCallResult{}, &toolExecutionError{Code: "INVALID_FIELD", Message: err.Error(), Retryable: false}
	}
	glob, err := parseOptionalString(args, "glob")
	if err != nil {
		return toolCallResult{}, &toolExecutionError{Code: "INVALID_FIELD", Message: err.Error(), Retryable: false}
	}

	limit := 200
	if rawLimit, ok := args["limit"]; ok {
		parsedLimit, parseErr := parseInteger(rawLimit, "limit")
		if parseErr != nil {
			return toolCallResult{}, &toolExecutionError{Code: "INVALID_FIELD", Message: parseErr.Error(), Retryable: false}
		}
		limit = parsedLimit
	}
	if limit < 1 || limit > 5000 {
		return toolCallResult{}, &toolExecutionError{Code: "INVALID_RANGE", Message: "limit must be between 1 and 5000", Retryable: false}
	}

	offset := 0
	if rawOffset, ok := args["offset"]; ok {
		parsedOffset, parseErr := parseInteger(rawOffset, "offset")
		if parseErr != nil {
			return toolCallResult{}, &toolExecutionError{Code: "INVALID_FIELD", Message: parseErr.Error(), Retryable: false}
		}
		offset = parsedOffset
	}
	if offset < 0 {
		return toolCallResult{}, &toolExecutionError{Code: "INVALID_RANGE", Message: "offset must be >= 0", Retryable: false}
	}

	var (
		docs  []model.Document
		total int64
	)
	if s.store == nil {
		docs = []model.Document{}
		total = 0
	} else {
		listedDocs, listedTotal, listErr := s.store.ListFiles(ctx, pathPrefix, glob, limit, offset)
		if listErr != nil && !errors.Is(listErr, model.ErrNotImplemented) {
			return toolCallResult{}, &toolExecutionError{
				Code:      "STORE_CORRUPT",
				Message:   listErr.Error(),
				Retryable: false,
			}
		}
		if listErr == nil {
			docs = listedDocs
			total = listedTotal
		}
	}

	files := make([]map[string]interface{}, 0, len(docs))
	for _, doc := range docs {
		status := normalizeFileStatus(doc.Status)
		files = append(files, map[string]interface{}{
			"rel_path":   doc.RelPath,
			"doc_type":   doc.DocType,
			"size_bytes": doc.SizeBytes,
			"mtime_unix": doc.MTimeUnix,
			"status":     status,
			"deleted":    doc.Deleted,
		})
	}

	structured := map[string]interface{}{
		"limit":  limit,
		"offset": offset,
		"total":  total,
		"files":  files,
	}

	text := fmt.Sprintf("listed %d file(s) (total=%d, limit=%d, offset=%d)", len(files), total, limit, offset)
	return toolCallResult{
		Content: []toolContentItem{
			{Type: "text", Text: text},
		},
		StructuredContent: structured,
	}, nil
}

func (s *Server) handleSearchTool(ctx context.Context, args map[string]interface{}) (toolCallResult, *toolExecutionError) {
	if err := assertNoUnknownArguments(args, map[string]struct{}{
		"query":       {},
		"k":           {},
		"index":       {},
		"path_prefix": {},
		"file_glob":   {},
		"doc_types":   {},
	}); err != nil {
		return toolCallResult{}, &toolExecutionError{Code: "INVALID_FIELD", Message: err.Error(), Retryable: false}
	}

	query, ok, err := parseRequiredString(args, "query")
	if err != nil {
		return toolCallResult{}, &toolExecutionError{Code: "INVALID_FIELD", Message: err.Error(), Retryable: false}
	}
	if !ok {
		return toolCallResult{}, &toolExecutionError{Code: "MISSING_FIELD", Message: "query is required", Retryable: false}
	}
	k := DefaultSearchK
	if rawK, exists := args["k"]; exists {
		parsedK, parseErr := parseInteger(rawK, "k")
		if parseErr != nil {
			return toolCallResult{}, &toolExecutionError{Code: "INVALID_FIELD", Message: parseErr.Error(), Retryable: false}
		}
		k = parsedK
	}
	if k <= 0 {
		k = DefaultSearchK
	}
	if k > 50 {
		return toolCallResult{}, &toolExecutionError{Code: "INVALID_RANGE", Message: "k must be between 1 and 50", Retryable: false}
	}

	indexName, err := parseOptionalString(args, "index")
	if err != nil {
		return toolCallResult{}, &toolExecutionError{Code: "INVALID_FIELD", Message: err.Error(), Retryable: false}
	}
	indexName = strings.ToLower(strings.TrimSpace(indexName))
	if indexName == "" {
		indexName = "auto"
	}
	switch indexName {
	case "auto", "text", "code", "both":
	default:
		return toolCallResult{}, &toolExecutionError{Code: "INVALID_FIELD", Message: "index must be one of auto,text,code,both", Retryable: false}
	}

	pathPrefix, err := parseOptionalString(args, "path_prefix")
	if err != nil {
		return toolCallResult{}, &toolExecutionError{Code: "INVALID_FIELD", Message: err.Error(), Retryable: false}
	}
	fileGlob, err := parseOptionalString(args, "file_glob")
	if err != nil {
		return toolCallResult{}, &toolExecutionError{Code: "INVALID_FIELD", Message: err.Error(), Retryable: false}
	}
	docTypes, err := parseOptionalStringSlice(args, "doc_types")
	if err != nil {
		return toolCallResult{}, &toolExecutionError{Code: "INVALID_FIELD", Message: err.Error(), Retryable: false}
	}

	if s.retriever == nil {
		return toolCallResult{}, &toolExecutionError{Code: "INDEX_NOT_READY", Message: "retriever not configured", Retryable: false}
	}
	hits, searchErr := s.retriever.Search(ctx, model.SearchQuery{
		Query:      query,
		K:          k,
		Index:      indexName,
		PathPrefix: pathPrefix,
		FileGlob:   fileGlob,
		DocTypes:   docTypes,
	})
	if searchErr != nil {
		code := "INTERNAL_ERROR"
		message := "internal server error"
		retryable := true
		if errors.Is(searchErr, model.ErrIndexNotReady) || errors.Is(searchErr, model.ErrIndexNotConfigured) {
			code = "INDEX_NOT_READY"
			message = "index not ready"
		}
		return toolCallResult{}, &toolExecutionError{Code: code, Message: message, Retryable: retryable}
	}

	indexUsed := "text"
	switch indexName {
	case "code":
		indexUsed = "code"
	case "both":
		indexUsed = "both"
	}

	structured := map[string]interface{}{
		"query":             query,
		"k":                 k,
		"index_used":        indexUsed,
		"hits":              hits,
		"indexing_complete": false,
	}

	return toolCallResult{
		Content: []toolContentItem{
			{Type: "text", Text: fmt.Sprintf("found %d result(s)", len(hits))},
		},
		StructuredContent: structured,
	}, nil
}

func (s *Server) handleAskTool(ctx context.Context, args map[string]interface{}) (toolCallResult, *toolExecutionError) {
	if err := assertNoUnknownArguments(args, map[string]struct{}{
		"question":    {},
		"k":           {},
		"mode":        {},
		"index":       {},
		"path_prefix": {},
		"file_glob":   {},
		"doc_types":   {},
	}); err != nil {
		return toolCallResult{}, &toolExecutionError{Code: "INVALID_FIELD", Message: err.Error(), Retryable: false}
	}

	question, ok, err := parseRequiredString(args, "question")
	if err != nil {
		return toolCallResult{}, &toolExecutionError{Code: "INVALID_FIELD", Message: err.Error(), Retryable: false}
	}
	if !ok {
		return toolCallResult{}, &toolExecutionError{Code: "MISSING_FIELD", Message: "question is required", Retryable: false}
	}

	if s.retriever == nil {
		return toolCallResult{}, &toolExecutionError{Code: "INDEX_NOT_READY", Message: "retriever not configured", Retryable: false}
	}

	// default k should stay in sync with the schema and other tools.  the
	// shared constant lives in server.go (DefaultSearchK == 10) so use that
	// instead of a hardcoded literal.
	k := DefaultSearchK
	if rawK, exists := args["k"]; exists {
		parsedK, parseErr := parseInteger(rawK, "k")
		if parseErr != nil {
			return toolCallResult{}, &toolExecutionError{Code: "INVALID_FIELD", Message: parseErr.Error(), Retryable: false}
		}
		// Mirror handleSearchTool behavior explicitly.
		if parsedK <= 0 {
			k = DefaultSearchK
		} else {
			k = parsedK
		}
	}
	if k > 50 {
		return toolCallResult{}, &toolExecutionError{Code: "INVALID_RANGE", Message: "k must be between 1 and 50", Retryable: false}
	}

	mode, err := parseOptionalString(args, "mode")
	if err != nil {
		return toolCallResult{}, &toolExecutionError{Code: "INVALID_FIELD", Message: err.Error(), Retryable: false}
	}
	mode = strings.ToLower(strings.TrimSpace(mode))
	if mode == "" {
		mode = "answer"
	}
	switch mode {
	case "answer", "search_only":
	default:
		return toolCallResult{}, &toolExecutionError{Code: "INVALID_FIELD", Message: "mode must be one of answer,search_only", Retryable: false}
	}

	indexName, err := parseOptionalString(args, "index")
	if err != nil {
		return toolCallResult{}, &toolExecutionError{Code: "INVALID_FIELD", Message: err.Error(), Retryable: false}
	}
	indexName = strings.ToLower(strings.TrimSpace(indexName))
	if indexName == "" {
		indexName = "auto"
	}
	switch indexName {
	case "auto", "text", "code", "both":
	default:
		return toolCallResult{}, &toolExecutionError{Code: "INVALID_FIELD", Message: "index must be one of auto,text,code,both", Retryable: false}
	}

	pathPrefix, err := parseOptionalString(args, "path_prefix")
	if err != nil {
		return toolCallResult{}, &toolExecutionError{Code: "INVALID_FIELD", Message: err.Error(), Retryable: false}
	}
	fileGlob, err := parseOptionalString(args, "file_glob")
	if err != nil {
		return toolCallResult{}, &toolExecutionError{Code: "INVALID_FIELD", Message: err.Error(), Retryable: false}
	}
	docTypes, err := parseOptionalStringSlice(args, "doc_types")
	if err != nil {
		return toolCallResult{}, &toolExecutionError{Code: "INVALID_FIELD", Message: err.Error(), Retryable: false}
	}

	askResult, askErr := s.retriever.Ask(ctx, question, model.SearchQuery{
		Query:      question,
		K:          k,
		Index:      indexName,
		PathPrefix: pathPrefix,
		FileGlob:   fileGlob,
		DocTypes:   docTypes,
	})
	if askErr != nil {
		code := "INTERNAL_ERROR"
		message := "internal server error"
		retryable := true
		if errors.Is(askErr, model.ErrIndexNotReady) || errors.Is(askErr, model.ErrIndexNotConfigured) {
			code = "INDEX_NOT_READY"
			message = "index not ready"
			// retryable stays true to encourage callers to retry once the
			// index becomes available, matching handleSearchTool/handleAskAudioTool
		}
		return toolCallResult{}, &toolExecutionError{Code: code, Message: message, Retryable: retryable}
	}
	if mode == "search_only" {
		askResult.Answer = ""
	}

	structured := map[string]interface{}{
		"question":          question,
		"answer":            askResult.Answer,
		"citations":         askResult.Citations,
		"hits":              askResult.Hits,
		"indexing_complete": askResult.IndexingComplete,
	}
	contentText := askResult.Answer
	if mode == "search_only" {
		contentText = fmt.Sprintf("found %d supporting result(s)", len(askResult.Hits))
	}

	return toolCallResult{
		Content:           []toolContentItem{{Type: "text", Text: contentText}},
		StructuredContent: structured,
	}, nil
}

func (s *Server) handleAskAudioTool(ctx context.Context, args map[string]interface{}) (toolCallResult, *toolExecutionError) {
	if err := assertNoUnknownArguments(args, map[string]struct{}{
		"question":    {},
		"k":           {},
		"mode":        {},
		"index":       {},
		"path_prefix": {},
		"file_glob":   {},
		"doc_types":   {},
		"voice_id":    {},
	}); err != nil {
		return toolCallResult{}, &toolExecutionError{Code: "INVALID_FIELD", Message: err.Error(), Retryable: false}
	}

	question, ok, err := parseRequiredString(args, "question")
	if err != nil {
		return toolCallResult{}, &toolExecutionError{Code: "INVALID_FIELD", Message: err.Error(), Retryable: false}
	}
	if !ok {
		return toolCallResult{}, &toolExecutionError{Code: "MISSING_FIELD", Message: "question is required", Retryable: false}
	}

	k := DefaultSearchK
	if rawK, exists := args["k"]; exists {
		parsedK, parseErr := parseInteger(rawK, "k")
		if parseErr != nil {
			return toolCallResult{}, &toolExecutionError{Code: "INVALID_FIELD", Message: parseErr.Error(), Retryable: false}
		}
		k = parsedK
	}
	if k <= 0 {
		k = DefaultSearchK
	}
	if k > 50 {
		return toolCallResult{}, &toolExecutionError{Code: "INVALID_RANGE", Message: "k must be between 1 and 50", Retryable: false}
	}

	mode, err := parseOptionalString(args, "mode")
	if err != nil {
		return toolCallResult{}, &toolExecutionError{Code: "INVALID_FIELD", Message: err.Error(), Retryable: false}
	}
	mode = strings.ToLower(strings.TrimSpace(mode))
	if mode == "" {
		mode = "answer"
	}
	switch mode {
	case "answer", "search_only":
	default:
		return toolCallResult{}, &toolExecutionError{Code: "INVALID_FIELD", Message: "mode must be one of answer,search_only", Retryable: false}
	}

	indexName, err := parseOptionalString(args, "index")
	if err != nil {
		return toolCallResult{}, &toolExecutionError{Code: "INVALID_FIELD", Message: err.Error(), Retryable: false}
	}
	indexName = strings.ToLower(strings.TrimSpace(indexName))
	if indexName == "" {
		indexName = "auto"
	}
	switch indexName {
	case "auto", "text", "code", "both":
	default:
		return toolCallResult{}, &toolExecutionError{Code: "INVALID_FIELD", Message: "index must be one of auto,text,code,both", Retryable: false}
	}

	pathPrefix, err := parseOptionalString(args, "path_prefix")
	if err != nil {
		return toolCallResult{}, &toolExecutionError{Code: "INVALID_FIELD", Message: err.Error(), Retryable: false}
	}
	fileGlob, err := parseOptionalString(args, "file_glob")
	if err != nil {
		return toolCallResult{}, &toolExecutionError{Code: "INVALID_FIELD", Message: err.Error(), Retryable: false}
	}
	docTypes, err := parseOptionalStringSlice(args, "doc_types")
	if err != nil {
		return toolCallResult{}, &toolExecutionError{Code: "INVALID_FIELD", Message: err.Error(), Retryable: false}
	}
	voiceID, err := parseOptionalString(args, "voice_id")
	if err != nil {
		return toolCallResult{}, &toolExecutionError{Code: "INVALID_FIELD", Message: err.Error(), Retryable: false}
	}

	if s.retriever == nil {
		return toolCallResult{}, &toolExecutionError{Code: "INDEX_NOT_READY", Message: "retriever not configured", Retryable: false}
	}

	if mode == "search_only" {
		hits, searchErr := s.retriever.Search(ctx, model.SearchQuery{
			Query:      question,
			K:          k,
			Index:      indexName,
			PathPrefix: pathPrefix,
			FileGlob:   fileGlob,
			DocTypes:   docTypes,
		})
		if searchErr != nil {
			switch {
			case errors.Is(searchErr, model.ErrIndexNotReady), errors.Is(searchErr, model.ErrIndexNotConfigured):
				return toolCallResult{}, &toolExecutionError{Code: "INDEX_NOT_READY", Message: "index not ready", Retryable: true}
			default:
				return toolCallResult{}, &toolExecutionError{Code: "INTERNAL_ERROR", Message: "internal server error", Retryable: true}
			}
		}
		hitMaps := make([]map[string]interface{}, 0, len(hits))
		for _, h := range hits {
			hitMaps = append(hitMaps, serializeHit(h))
		}
		structured := map[string]interface{}{
			"question":          question,
			"answer":            "",
			"citations":         []interface{}{},
			"hits":              hitMaps,
			"indexing_complete": false,
		}
		return toolCallResult{
			Content: []toolContentItem{
				{Type: "text", Text: fmt.Sprintf("found %d results for %q", len(hits), question)},
			},
			StructuredContent: structured,
		}, nil
	}

	askResult, askErr := s.retriever.Ask(ctx, question, model.SearchQuery{
		Query:      question,
		K:          k,
		Index:      indexName,
		PathPrefix: pathPrefix,
		FileGlob:   fileGlob,
		DocTypes:   docTypes,
	})
	if askErr != nil {
		switch {
		case errors.Is(askErr, model.ErrNotImplemented):
			fallbackStructured := map[string]interface{}{
				"question":          question,
				"answer":            "",
				"citations":         []interface{}{},
				"hits":              []interface{}{},
				"indexing_complete": false,
			}
			return toolCallResult{
				Content: []toolContentItem{
					{Type: "text", Text: "ask_audio is not available yet; use dir2mcp.search while ask generation is being implemented"},
				},
				StructuredContent: fallbackStructured,
			}, nil
		case errors.Is(askErr, model.ErrIndexNotReady), errors.Is(askErr, model.ErrIndexNotConfigured):
			return toolCallResult{}, &toolExecutionError{Code: "INDEX_NOT_READY", Message: "index not ready", Retryable: true}
		default:
			return toolCallResult{}, &toolExecutionError{Code: "INTERNAL_ERROR", Message: "internal server error", Retryable: true}
		}
	}

	if strings.TrimSpace(askResult.Question) == "" {
		askResult.Question = question
	}
	structured := buildAskStructuredContent(askResult)

	answerText := strings.TrimSpace(askResult.Answer)
	if answerText == "" {
		answerText = "no answer text returned"
	}

	if s.tts == nil {
		text := answerText + "\n\nAudio synthesis is disabled. Set ELEVENLABS_API_KEY to enable dir2mcp.ask_audio voice output."
		return toolCallResult{
			Content: []toolContentItem{
				{Type: "text", Text: text},
			},
			StructuredContent: structured,
		}, nil
	}

	var (
		audioBytes []byte
		synthErr   error
	)
	if voiceID != "" {
		if voiceAware, ok := s.tts.(voiceAwareTTSSynthesizer); ok {
			audioBytes, synthErr = voiceAware.SynthesizeWithVoice(ctx, answerText, voiceID)
		} else {
			audioBytes, synthErr = s.tts.Synthesize(ctx, answerText)
		}
	} else {
		audioBytes, synthErr = s.tts.Synthesize(ctx, answerText)
	}

	if synthErr != nil {
		return toolCallResult{
			Content: []toolContentItem{
				{Type: "text", Text: answerText + "\n\nAudio synthesis failed, returning text-only response."},
			},
			StructuredContent: structured,
		}, nil
	}

	encodedAudio := base64.StdEncoding.EncodeToString(audioBytes)
	structured["audio"] = map[string]interface{}{
		"mime_type": "audio/mpeg",
		"data":      encodedAudio,
	}

	return toolCallResult{
		Content: []toolContentItem{
			{Type: "text", Text: answerText},
			{Type: "audio", MIMEType: "audio/mpeg", Data: encodedAudio},
		},
		StructuredContent: structured,
	}, nil
}

func (s *Server) handleOpenFileTool(ctx context.Context, args map[string]interface{}) (toolCallResult, *toolExecutionError) {
	if err := assertNoUnknownArguments(args, map[string]struct{}{
		"rel_path":   {},
		"start_line": {},
		"end_line":   {},
		"page":       {},
		"start_ms":   {},
		"end_ms":     {},
		"max_chars":  {},
	}); err != nil {
		return toolCallResult{}, &toolExecutionError{Code: "INVALID_FIELD", Message: err.Error(), Retryable: false}
	}

	relPath, ok, err := parseRequiredString(args, "rel_path")
	if err != nil {
		return toolCallResult{}, &toolExecutionError{Code: "INVALID_FIELD", Message: err.Error(), Retryable: false}
	}
	if !ok {
		return toolCallResult{}, &toolExecutionError{Code: "MISSING_FIELD", Message: "rel_path is required", Retryable: false}
	}

	if s.retriever == nil {
		return toolCallResult{}, &toolExecutionError{Code: "INDEX_NOT_READY", Message: "retriever not configured", Retryable: false}
	}

	maxChars := 20000
	if raw, ok := args["max_chars"]; ok {
		parsed, parseErr := parseInteger(raw, "max_chars")
		if parseErr != nil {
			return toolCallResult{}, &toolExecutionError{Code: "INVALID_FIELD", Message: parseErr.Error(), Retryable: false}
		}
		maxChars = parsed
	}
	if maxChars < 200 || maxChars > 50000 {
		return toolCallResult{}, &toolExecutionError{Code: "INVALID_FIELD", Message: "max_chars must be between 200 and 50000", Retryable: false}
	}

	// parse all span-related parameters so we can detect conflicts between groups
	span := model.Span{}
	// group A: page
	hasPage := false
	var page int
	if raw, ok := args["page"]; ok {
		hasPage = true
		var parseErr error
		page, parseErr = parseInteger(raw, "page")
		if parseErr != nil {
			return toolCallResult{}, &toolExecutionError{Code: "INVALID_FIELD", Message: parseErr.Error(), Retryable: false}
		}
		if page <= 0 {
			return toolCallResult{}, &toolExecutionError{Code: "INVALID_FIELD", Message: "page must be > 0", Retryable: false}
		}
	}

	// group B: start_ms/end_ms
	startMS, hasStartMS, err := parseOptionalIntegerWithPresence(args, "start_ms")
	if err != nil {
		return toolCallResult{}, &toolExecutionError{Code: "INVALID_FIELD", Message: err.Error(), Retryable: false}
	}
	endMS, hasEndMS, err := parseOptionalIntegerWithPresence(args, "end_ms")
	if err != nil {
		return toolCallResult{}, &toolExecutionError{Code: "INVALID_FIELD", Message: err.Error(), Retryable: false}
	}

	// group C: start_line/end_line
	startLine, hasStartLine, err := parseOptionalIntegerWithPresence(args, "start_line")
	if err != nil {
		return toolCallResult{}, &toolExecutionError{Code: "INVALID_FIELD", Message: err.Error(), Retryable: false}
	}
	endLine, hasEndLine, err := parseOptionalIntegerWithPresence(args, "end_line")
	if err != nil {
		return toolCallResult{}, &toolExecutionError{Code: "INVALID_FIELD", Message: err.Error(), Retryable: false}
	}

	// detect mutually-exclusive groups: page vs time vs lines
	groups := 0
	if hasPage {
		groups++
	}
	if hasStartMS || hasEndMS {
		groups++
	}
	if hasStartLine || hasEndLine {
		groups++
	}
	if groups > 1 {
		return toolCallResult{}, &toolExecutionError{Code: "INVALID_FIELD", Message: "conflicting span parameters: provide only one of page, start_ms/end_ms, or start_line/end_line", Retryable: false}
	}

	// now build the span based on the single group present (if any)
	if hasPage {
		span = model.Span{Kind: "page", Page: page}
	} else if hasStartMS || hasEndMS {
		// require both parameters when specifying a time span
		if hasStartMS != hasEndMS {
			return toolCallResult{}, &toolExecutionError{Code: "INVALID_FIELD", Message: "both start_ms and end_ms must be provided", Retryable: false}
		}
		if (hasStartMS && startMS < 0) || (hasEndMS && endMS < 0) {
			return toolCallResult{}, &toolExecutionError{Code: "INVALID_FIELD", Message: "start_ms/end_ms must be >= 0", Retryable: false}
		}
		if hasStartMS && hasEndMS && startMS > endMS {
			return toolCallResult{}, &toolExecutionError{Code: "INVALID_FIELD", Message: "start_ms must be <= end_ms", Retryable: false}
		}
		span = model.Span{Kind: "time", StartMS: startMS, EndMS: endMS}
	} else if hasStartLine || hasEndLine {
		// runtime validation mirrors openFileInputSchema which requires
		// positive line numbers; do not allow zero or negative values.
		if hasStartLine != hasEndLine {
			return toolCallResult{}, &toolExecutionError{Code: "INVALID_FIELD", Message: "both start_line and end_line must be provided", Retryable: false}
		}
		if (hasStartLine && startLine <= 0) || (hasEndLine && endLine <= 0) {
			return toolCallResult{}, &toolExecutionError{Code: "INVALID_FIELD", Message: "start_line/end_line must be > 0", Retryable: false}
		}
		if hasStartLine && hasEndLine && startLine > endLine {
			return toolCallResult{}, &toolExecutionError{Code: "INVALID_FIELD", Message: "start_line must be <= end_line", Retryable: false}
		}
		span = model.Span{Kind: "lines", StartLine: startLine, EndLine: endLine}
	}

	var (
		content   string
		truncated bool
		openErr   error
	)
	if withMeta, ok := s.retriever.(retrieverOpenFileWithMeta); ok {
		content, truncated, openErr = withMeta.OpenFileWithMeta(ctx, relPath, span, maxChars)
	} else {
		content, openErr = s.retriever.OpenFile(ctx, relPath, span, maxChars)
		truncated = len([]rune(content)) > maxChars
	}
	if openErr != nil {
		switch {
		case errors.Is(openErr, model.ErrForbidden):
			return toolCallResult{}, &toolExecutionError{Code: "FORBIDDEN", Message: "forbidden", Retryable: false}
		case errors.Is(openErr, model.ErrPathOutsideRoot):
			return toolCallResult{}, &toolExecutionError{Code: "PATH_OUTSIDE_ROOT", Message: "path outside root", Retryable: false}
		case errors.Is(openErr, model.ErrDocTypeUnsupported):
			return toolCallResult{}, &toolExecutionError{Code: "DOC_TYPE_UNSUPPORTED", Message: "doc type unsupported", Retryable: false}
		case errors.Is(openErr, os.ErrNotExist):
			return toolCallResult{}, &toolExecutionError{Code: "NOT_FOUND", Message: "file not found", Retryable: false}
		default:
			return toolCallResult{}, &toolExecutionError{Code: "INTERNAL_ERROR", Message: "internal server error", Retryable: true}
		}
	}

	structured := map[string]interface{}{
		"rel_path":  relPath,
		"doc_type":  inferDocType(relPath),
		"content":   content,
		"truncated": truncated,
	}
	if strings.TrimSpace(span.Kind) != "" {
		structured["span"] = buildOpenFileSpan(span)
	}

	return toolCallResult{
		Content: []toolContentItem{
			{Type: "text", Text: content},
		},
		StructuredContent: structured,
	}, nil
}

func assertNoUnknownArguments(args map[string]interface{}, allowed map[string]struct{}) error {
	for key := range args {
		if _, ok := allowed[key]; !ok {
			return fmt.Errorf("unknown argument: %s", key)
		}
	}
	return nil
}

func parseRequiredString(args map[string]interface{}, key string) (string, bool, error) {
	raw, ok := args[key]
	if !ok {
		return "", false, nil
	}
	value, ok := raw.(string)
	if !ok {
		return "", true, fmt.Errorf("%s must be a string", key)
	}
	value = strings.TrimSpace(value)
	if value == "" {
		return "", true, fmt.Errorf("%s must be a non-empty string", key)
	}
	return value, true, nil
}

func parseOptionalString(args map[string]interface{}, key string) (string, error) {
	raw, ok := args[key]
	if !ok {
		return "", nil
	}
	value, ok := raw.(string)
	if !ok {
		return "", fmt.Errorf("%s must be a string", key)
	}
	return strings.TrimSpace(value), nil
}

func inferDocType(relPath string) string {
	ext := strings.ToLower(filepath.Ext(strings.TrimSpace(relPath)))
	switch ext {
	case ".go", ".js", ".jsx", ".ts", ".tsx",
		".py", ".java", ".rb", ".cpp", ".c", ".cs",
		".kt", ".kts", ".swift", ".php", ".scala", ".rs",
		".h", ".hpp", ".hh", ".m", ".mm", ".dart",
		".pl", ".pm", ".lua", ".r", ".jl", ".hs",
		".erl", ".ex", ".exs", ".sql", ".sh", ".zsh",
		".fish":
		return "code"
	case ".html", ".htm", ".css":
		return "html"
	case ".md":
		return "md"
	case ".txt", ".rst":
		return "text"
	case ".pdf":
		return "pdf"
	case ".mp3", ".wav", ".m4a", ".flac":
		return "audio"
	case ".png", ".jpg", ".jpeg", ".gif", ".webp":
		return "image"
	default:
		return "unknown"
	}
}

func buildOpenFileSpan(span model.Span) map[string]interface{} {
	kind := strings.TrimSpace(span.Kind)
	switch kind {
	case "lines":
		return map[string]interface{}{
			"kind":       "lines",
			"start_line": span.StartLine,
			"end_line":   span.EndLine,
		}
	case "page":
		return map[string]interface{}{
			"kind": "page",
			"page": span.Page,
		}
	case "time":
		return map[string]interface{}{
			"kind":     "time",
			"start_ms": span.StartMS,
			"end_ms":   span.EndMS,
		}
	default:
		return map[string]interface{}{
			"kind": kind,
		}
	}
}

func parseInteger(value interface{}, field string) (int, error) {
	switch v := value.(type) {
	case float64:
		if math.Trunc(v) != v {
			return 0, fmt.Errorf("%s must be an integer", field)
		}
		if v < math.MinInt || v > math.MaxInt {
			return 0, fmt.Errorf("%s is out of range", field)
		}
		return int(v), nil
	case int:
		return v, nil
	case int64:
		if v < math.MinInt || v > math.MaxInt {
			return 0, fmt.Errorf("%s is out of range", field)
		}
		return int(v), nil
	default:
		return 0, fmt.Errorf("%s must be an integer", field)
	}
}

func parseOptionalIntegerWithPresence(args map[string]interface{}, key string) (int, bool, error) {
	raw, ok := args[key]
	if !ok {
		return 0, false, nil
	}
	v, err := parseInteger(raw, key)
	if err != nil {
		return 0, true, err
	}
	return v, true, nil
}

func parseOptionalStringSlice(args map[string]interface{}, key string) ([]string, error) {
	raw, ok := args[key]
	if !ok || raw == nil {
		return nil, nil
	}

	switch typed := raw.(type) {
	case []interface{}:
		out := make([]string, 0, len(typed))
		for idx, item := range typed {
			v, ok := item.(string)
			if !ok {
				return nil, fmt.Errorf("%s[%d] must be a string", key, idx)
			}
			v = strings.TrimSpace(v)
			if v == "" {
				return nil, fmt.Errorf("%s[%d] must be a non-empty string", key, idx)
			}
			out = append(out, v)
		}
		return out, nil
	case []string:
		out := make([]string, 0, len(typed))
		for idx, item := range typed {
			item = strings.TrimSpace(item)
			if item == "" {
				return nil, fmt.Errorf("%s[%d] must be a non-empty string", key, idx)
			}
			out = append(out, item)
		}
		return out, nil
	default:
		return nil, fmt.Errorf("%s must be an array of strings", key)
	}
}

func normalizeFileStatus(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "skipped":
		return "skipped"
	case "error":
		return "error"
	default:
		return "ok"
	}
}

func serializeHit(h model.SearchHit) map[string]interface{} {
	return map[string]interface{}{
		"chunk_id": h.ChunkID,
		"rel_path": h.RelPath,
		"doc_type": h.DocType,
		"rep_type": h.RepType,
		"score":    h.Score,
		"snippet":  h.Snippet,
		"span":     buildOpenFileSpan(h.Span),
	}
}

func buildAskStructuredContent(result model.AskResult) map[string]interface{} {
	citations := make([]map[string]interface{}, 0, len(result.Citations))
	for _, citation := range result.Citations {
		citations = append(citations, map[string]interface{}{
			"chunk_id": citation.ChunkID,
			"rel_path": citation.RelPath,
			"span":     buildOpenFileSpan(citation.Span),
		})
	}

	hits := make([]map[string]interface{}, 0, len(result.Hits))
	for _, hit := range result.Hits {
		hits = append(hits, serializeHit(hit))
	}

	return map[string]interface{}{
		"question":          result.Question,
		"answer":            result.Answer,
		"citations":         citations,
		"hits":              hits,
		"indexing_complete": result.IndexingComplete,
	}
}

func spanDefinitionSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"oneOf": []interface{}{
			map[string]interface{}{
				"type":                 "object",
				"additionalProperties": false,
				"properties": map[string]interface{}{
					"kind":       map[string]interface{}{"const": "lines"},
					"start_line": map[string]interface{}{"type": "integer"},
					"end_line":   map[string]interface{}{"type": "integer"},
				},
				"required": []string{"kind", "start_line", "end_line"},
			},
			map[string]interface{}{
				"type":                 "object",
				"additionalProperties": false,
				"properties": map[string]interface{}{
					"kind": map[string]interface{}{"const": "page"},
					"page": map[string]interface{}{"type": "integer"},
				},
				"required": []string{"kind", "page"},
			},
			map[string]interface{}{
				"type":                 "object",
				"additionalProperties": false,
				"properties": map[string]interface{}{
					"kind":     map[string]interface{}{"const": "time"},
					"start_ms": map[string]interface{}{"type": "integer"},
					"end_ms":   map[string]interface{}{"type": "integer"},
				},
				"required": []string{"kind", "start_ms", "end_ms"},
			},
		},
	}
}

func hitDefinitionSchema() map[string]interface{} {
	return map[string]interface{}{
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]interface{}{
			"chunk_id": map[string]interface{}{"type": "integer"},
			"rel_path": map[string]interface{}{"type": "string"},
			"doc_type": map[string]interface{}{"type": "string"},
			"rep_type": map[string]interface{}{"type": "string"},
			"score":    map[string]interface{}{"type": "number"},
			"snippet":  map[string]interface{}{"type": "string"},
			"span":     map[string]interface{}{"$ref": "#/definitions/Span"},
		},
		"required": []string{"chunk_id", "rel_path", "score", "snippet", "span"},
	}
}

func sharedDefinitions() map[string]interface{} {
	return map[string]interface{}{
		"Span": spanDefinitionSchema(),
		"Hit":  hitDefinitionSchema(),
	}
}

func searchInputSchema() map[string]interface{} {
	return map[string]interface{}{
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]interface{}{
			"query":       map[string]interface{}{"type": "string", "minLength": 1},
			"k":           map[string]interface{}{"type": "integer", "minimum": 1, "maximum": 50, "default": 10},
			"index":       map[string]interface{}{"type": "string", "enum": []string{"auto", "text", "code", "both"}, "default": "auto"},
			"path_prefix": map[string]interface{}{"type": "string"},
			"file_glob":   map[string]interface{}{"type": "string"},
			"doc_types":   map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}},
		},
		"required": []string{"query"},
	}
}

func searchOutputSchema() map[string]interface{} {
	return map[string]interface{}{
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]interface{}{
			"query":             map[string]interface{}{"type": "string"},
			"k":                 map[string]interface{}{"type": "integer"},
			"index_used":        map[string]interface{}{"type": "string", "enum": []string{"text", "code", "both"}},
			"hits":              map[string]interface{}{"type": "array", "items": map[string]interface{}{"$ref": "#/definitions/Hit"}},
			"indexing_complete": map[string]interface{}{"type": "boolean"},
		},
		"required":    []string{"query", "hits", "indexing_complete"},
		"definitions": sharedDefinitions(),
	}
}

func askInputSchema() map[string]interface{} {
	return map[string]interface{}{
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]interface{}{
			"question":    map[string]interface{}{"type": "string", "minLength": 1},
			"k":           map[string]interface{}{"type": "integer", "minimum": 1, "maximum": 50, "default": 10},
			"mode":        map[string]interface{}{"type": "string", "enum": []string{"answer", "search_only"}, "default": "answer"},
			"index":       map[string]interface{}{"type": "string", "enum": []string{"auto", "text", "code", "both"}, "default": "auto"},
			"path_prefix": map[string]interface{}{"type": "string"},
			"file_glob":   map[string]interface{}{"type": "string"},
			"doc_types":   map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}},
		},
		"required": []string{"question"},
	}
}

func askOutputSchema() map[string]interface{} {
	return map[string]interface{}{
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]interface{}{
			"question": map[string]interface{}{"type": "string"},
			"answer":   map[string]interface{}{"type": "string"},
			"citations": map[string]interface{}{
				"type": "array",
				"items": map[string]interface{}{
					"type":                 "object",
					"additionalProperties": false,
					"properties": map[string]interface{}{
						"chunk_id": map[string]interface{}{"type": "integer"},
						"rel_path": map[string]interface{}{"type": "string"},
						"span":     map[string]interface{}{"$ref": "#/definitions/Span"},
					},
					"required": []string{"chunk_id", "rel_path", "span"},
				},
			},
			"hits":              map[string]interface{}{"type": "array", "items": map[string]interface{}{"$ref": "#/definitions/Hit"}},
			"indexing_complete": map[string]interface{}{"type": "boolean"},
		},
		"required":    []string{"question", "citations", "hits", "indexing_complete"},
		"definitions": sharedDefinitions(),
	}
}

func askAudioInputSchema() map[string]interface{} {
	schema := askInputSchema()
	properties, ok := schema["properties"].(map[string]interface{})
	if !ok {
		return askInputSchema()
	}
	properties["voice_id"] = map[string]interface{}{"type": "string", "minLength": 1}
	return schema
}

func askAudioOutputSchema() map[string]interface{} {
	schema := askOutputSchema()
	properties, ok := schema["properties"].(map[string]interface{})
	if !ok {
		return askOutputSchema()
	}
	properties["audio"] = map[string]interface{}{
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]interface{}{
			"mime_type": map[string]interface{}{"type": "string", "enum": []string{"audio/mpeg"}},
			"data":      map[string]interface{}{"type": "string"},
		},
		"required": []string{"mime_type", "data"},
	}
	return schema
}

func openFileInputSchema() map[string]interface{} {
	return map[string]interface{}{
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]interface{}{
			"rel_path":   map[string]interface{}{"type": "string", "minLength": 1},
			"start_line": map[string]interface{}{"type": "integer", "minimum": 1},
			"end_line":   map[string]interface{}{"type": "integer", "minimum": 1},
			"page":       map[string]interface{}{"type": "integer", "minimum": 1},
			"start_ms":   map[string]interface{}{"type": "integer", "minimum": 0},
			"end_ms":     map[string]interface{}{"type": "integer", "minimum": 0},
			"max_chars":  map[string]interface{}{"type": "integer", "minimum": 200, "maximum": 50000, "default": 20000},
		},
		"required": []string{"rel_path"},
	}
}

func openFileOutputSchema() map[string]interface{} {
	return map[string]interface{}{
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]interface{}{
			"rel_path":  map[string]interface{}{"type": "string"},
			"doc_type":  map[string]interface{}{"type": "string"},
			"span":      map[string]interface{}{"$ref": "#/definitions/Span"},
			"content":   map[string]interface{}{"type": "string"},
			"truncated": map[string]interface{}{"type": "boolean"},
		},
		"required":    []string{"rel_path", "doc_type", "content", "truncated"},
		"definitions": sharedDefinitions(),
	}
}

func listFilesInputSchema() map[string]interface{} {
	return map[string]interface{}{
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]interface{}{
			"path_prefix": map[string]interface{}{"type": "string"},
			"glob":        map[string]interface{}{"type": "string"},
			"limit":       map[string]interface{}{"type": "integer", "minimum": 1, "maximum": 5000, "default": 200},
			"offset":      map[string]interface{}{"type": "integer", "minimum": 0, "default": 0},
		},
	}
}

func listFilesOutputSchema() map[string]interface{} {
	return map[string]interface{}{
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]interface{}{
			"limit":  map[string]interface{}{"type": "integer"},
			"offset": map[string]interface{}{"type": "integer"},
			"total":  map[string]interface{}{"type": "integer"},
			"files": map[string]interface{}{
				"type": "array",
				"items": map[string]interface{}{
					"type":                 "object",
					"additionalProperties": false,
					"properties": map[string]interface{}{
						"rel_path":   map[string]interface{}{"type": "string"},
						"doc_type":   map[string]interface{}{"type": "string"},
						"size_bytes": map[string]interface{}{"type": "integer"},
						"mtime_unix": map[string]interface{}{"type": "integer"},
						"status":     map[string]interface{}{"type": "string", "enum": []string{"ok", "skipped", "error"}},
						"deleted":    map[string]interface{}{"type": "boolean"},
					},
					"required": []string{"rel_path", "doc_type", "size_bytes", "mtime_unix", "status", "deleted"},
				},
			},
		},
		"required": []string{"limit", "offset", "total", "files"},
	}
}

func statsInputSchema() map[string]interface{} {
	return map[string]interface{}{
		"type":                 "object",
		"additionalProperties": false,
	}
}

func statsOutputSchema() map[string]interface{} {
	return map[string]interface{}{
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]interface{}{
			"root":             map[string]interface{}{"type": "string"},
			"state_dir":        map[string]interface{}{"type": "string"},
			"protocol_version": map[string]interface{}{"type": "string"},
			"indexing": map[string]interface{}{
				"type":                 "object",
				"additionalProperties": false,
				"properties": map[string]interface{}{
					"job_id":          map[string]interface{}{"type": "string"},
					"running":         map[string]interface{}{"type": "boolean"},
					"mode":            map[string]interface{}{"type": "string", "enum": []string{"incremental", "full"}},
					"scanned":         map[string]interface{}{"type": "integer"},
					"indexed":         map[string]interface{}{"type": "integer"},
					"skipped":         map[string]interface{}{"type": "integer"},
					"deleted":         map[string]interface{}{"type": "integer"},
					"representations": map[string]interface{}{"type": "integer"},
					"chunks_total":    map[string]interface{}{"type": "integer"},
					"embedded_ok":     map[string]interface{}{"type": "integer"},
					"errors":          map[string]interface{}{"type": "integer"},
				},
				"required": []string{"job_id", "running", "mode", "scanned", "indexed", "skipped", "deleted", "representations", "chunks_total", "embedded_ok", "errors"},
			},
			"models": map[string]interface{}{
				"type":                 "object",
				"additionalProperties": false,
				"properties": map[string]interface{}{
					"embed_text":   map[string]interface{}{"type": "string"},
					"embed_code":   map[string]interface{}{"type": "string"},
					"ocr":          map[string]interface{}{"type": "string"},
					"stt_provider": map[string]interface{}{"type": "string", "enum": []string{"mistral", "elevenlabs"}},
					"stt_model":    map[string]interface{}{"type": "string"},
					"chat":         map[string]interface{}{"type": "string"},
				},
				"required": []string{"embed_text", "embed_code", "ocr", "stt_provider", "stt_model", "chat"},
			},
		},
		"required": []string{"root", "state_dir", "protocol_version", "indexing", "models"},
	}
}
