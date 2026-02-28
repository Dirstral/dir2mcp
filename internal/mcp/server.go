package mcp

import (
	"context"
	"net"
	"net/http"
	"time"

	"github.com/Dirstral/dir2mcp/internal/config"
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
		_, _ = w.Write([]byte("MCP server not implemented yet"))
	})
	srv := &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	return srv.Serve(listener)
}
