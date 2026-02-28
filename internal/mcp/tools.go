package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"sort"
	"strings"

	"dir2mcp/internal/model"
)

const (
	toolNameSearch    = "dir2mcp.search"
	toolNameAsk       = "dir2mcp.ask"
	toolNameOpenFile  = "dir2mcp.open_file"
	toolNameListFiles = "dir2mcp.list_files"
	toolNameStats     = "dir2mcp.stats"

	defaultEmbedTextModel = "mistral-embed"
	defaultEmbedCodeModel = "codestral-embed"
	defaultOCRModel       = "mistral-ocr-latest"
	defaultSTTProvider    = "mistral"
	defaultSTTModel       = "voxtral-mini-latest"
	defaultChatModel      = "mistral-small-2506"
)

var toolOrder = []string{
	toolNameSearch,
	toolNameAsk,
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
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

type toolExecutionError struct {
	Code      string
	Message   string
	Retryable bool
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

func (s *Server) handleAskTool(_ context.Context, args map[string]interface{}) (toolCallResult, *toolExecutionError) {
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

	structured := map[string]interface{}{
		"question":          question,
		"answer":            "",
		"citations":         []interface{}{},
		"hits":              []interface{}{},
		"indexing_complete": false,
	}

	return toolCallResult{
		Content: []toolContentItem{
			{Type: "text", Text: "ask stub is registered; answer generation is not wired yet"},
		},
		StructuredContent: structured,
	}, nil
}

func (s *Server) handleOpenFileTool(_ context.Context, args map[string]interface{}) (toolCallResult, *toolExecutionError) {
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

	structured := map[string]interface{}{
		"rel_path":  relPath,
		"doc_type":  "unknown",
		"content":   "open_file stub is registered; file slicing is not wired yet",
		"truncated": false,
	}

	return toolCallResult{
		Content: []toolContentItem{
			{Type: "text", Text: "open_file stub is registered; file access is not wired yet"},
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
