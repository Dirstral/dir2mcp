package mcp

import (
	"context"
	"net"
	"net/http"

	"github.com/dir2mcp/dir2mcp/internal/config"
)

// ServerOptions for running the MCP server.
type ServerOptions struct {
	RootDir   string
	StateDir  string
	Config    *config.Config
	McpPath   string
	AuthToken string
}

// Server is a minimal stub until Ali implements the full MCP Streamable HTTP server.
type Server struct {
	opts ServerOptions
}

// NewServer creates an MCP server (stub: just blocks on Serve).
func NewServer(opts ServerOptions) (*Server, error) {
	return &Server{opts: opts}, nil
}

// RunIndexer runs background indexing (stub: no-op until Tia/Ark implement).
func (s *Server) RunIndexer(ctx context.Context) {}

// Serve blocks while handling HTTP. Stub: responds 501 on the MCP path.
func (s *Server) Serve(listener net.Listener) error {
	mux := http.NewServeMux()
	mux.HandleFunc(s.opts.McpPath, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotImplemented)
		w.Write([]byte("MCP server not implemented yet"))
	})
	return http.Serve(listener, mux)
}
