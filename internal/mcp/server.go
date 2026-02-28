package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/Dirstral/dir2mcp/internal/config"
	"github.com/Dirstral/dir2mcp/internal/model"
)

type Server struct {
	cfg       config.Config
	retriever model.Retriever
}

func NewServer(cfg config.Config, retriever model.Retriever) *Server {
	return &Server{
		cfg:       cfg,
		retriever: retriever,
	}
}

type jsonRPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type jsonRPCResponse struct {
	JSONRPC string           `json:"jsonrpc"`
	ID      any              `json:"id,omitempty"`
	Result  any              `json:"result,omitempty"`
	Error   *jsonRPCErrorObj `json:"error,omitempty"`
}

type jsonRPCErrorObj struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type toolsCallParams struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

type searchArgs struct {
	Query      string   `json:"query"`
	K          int      `json:"k"`
	Index      string   `json:"index"`
	PathPrefix string   `json:"path_prefix"`
	FileGlob   string   `json:"file_glob"`
	DocTypes   []string `json:"doc_types"`
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc(s.cfg.MCPPath, s.handleMCP)
	return mux
}

func (s *Server) Run(ctx context.Context) error {
	_ = ctx
	return model.ErrNotImplemented
}

func (s *Server) handleMCP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req jsonRPCRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, nil, -32700, "parse error")
		return
	}
	if req.JSONRPC != "2.0" {
		s.writeError(w, req.ID, -32600, "invalid request")
		return
	}

	switch req.Method {
	case "initialize":
		s.writeResult(w, req.ID, map[string]any{
			"protocolVersion": s.cfg.ProtocolVersion,
			"serverInfo": map[string]any{
				"name":    "dir2mcp",
				"version": "0.0.0-dev",
			},
			"capabilities": map[string]any{
				"tools": map[string]any{},
			},
		})
	case "notifications/initialized":
		w.WriteHeader(http.StatusAccepted)
	case "tools/list":
		s.handleToolsList(w, req.ID)
	case "tools/call":
		s.handleToolsCall(r.Context(), w, req.ID, req.Params)
	default:
		s.writeError(w, req.ID, -32601, "method not found")
	}
}

func (s *Server) handleToolsList(w http.ResponseWriter, id any) {
	tool := map[string]any{
		"name":        "dir2mcp.search",
		"description": "Semantic retrieval across indexed content.",
		"inputSchema": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"query":       map[string]any{"type": "string"},
				"k":           map[string]any{"type": "integer"},
				"index":       map[string]any{"type": "string", "enum": []string{"auto", "text", "code", "both"}},
				"path_prefix": map[string]any{"type": "string"},
				"file_glob":   map[string]any{"type": "string"},
				"doc_types": map[string]any{
					"type":  "array",
					"items": map[string]any{"type": "string"},
				},
			},
			"required": []string{"query"},
		},
	}

	s.writeResult(w, id, map[string]any{
		"tools": []any{tool},
	})
}

func (s *Server) handleToolsCall(ctx context.Context, w http.ResponseWriter, id any, raw json.RawMessage) {
	if s.retriever == nil {
		s.writeError(w, id, -32000, "retriever not configured")
		return
	}

	var params toolsCallParams
	if err := json.Unmarshal(raw, &params); err != nil {
		s.writeError(w, id, -32602, "invalid params")
		return
	}

	switch params.Name {
	case "dir2mcp.search":
		var args searchArgs
		if len(params.Arguments) > 0 {
			if err := json.Unmarshal(params.Arguments, &args); err != nil {
				s.writeError(w, id, -32602, "invalid tool arguments")
				return
			}
		}
		if strings.TrimSpace(args.Query) == "" {
			s.writeError(w, id, -32602, "query is required")
			return
		}
		hits, err := s.retriever.Search(ctx, model.SearchQuery{
			Query:      args.Query,
			K:          args.K,
			Index:      args.Index,
			PathPrefix: args.PathPrefix,
			FileGlob:   args.FileGlob,
			DocTypes:   args.DocTypes,
		})
		if err != nil {
			s.writeError(w, id, -32000, err.Error())
			return
		}

		contentText := fmt.Sprintf("Found %d hit(s) for query %q", len(hits), args.Query)
		s.writeResult(w, id, map[string]any{
			"content": []any{
				map[string]any{
					"type": "text",
					"text": contentText,
				},
			},
			"structuredContent": map[string]any{
				"query":             args.Query,
				"hits":              hits,
				"indexing_complete": false,
			},
		})
	default:
		s.writeError(w, id, -32602, "unknown tool")
	}
}

func (s *Server) writeResult(w http.ResponseWriter, id any, result any) {
	w.Header().Set("Content-Type", "application/json")
	resp := jsonRPCResponse{
		JSONRPC: "2.0",
		ID:      id,
		Result:  result,
	}
	_ = json.NewEncoder(w).Encode(resp)
}

func (s *Server) writeError(w http.ResponseWriter, id any, code int, message string) {
	w.Header().Set("Content-Type", "application/json")
	resp := jsonRPCResponse{
		JSONRPC: "2.0",
		ID:      id,
		Error: &jsonRPCErrorObj{
			Code:    code,
			Message: message,
		},
	}
	_ = json.NewEncoder(w).Encode(resp)
}
