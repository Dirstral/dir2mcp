package mcp

import (
	"context"
	"net"
	"net/http"
	"time"

	"dir2mcp/internal/config"
)

// ServerOptions configures the MCP server. McpPath is the HTTP path (e.g. /mcp).
type ServerOptions struct {
	RootDir   string
	StateDir  string
	Config    *config.Config
	McpPath   string
	AuthToken string
}

// Server is a minimal stub until full MCP Streamable HTTP is integrated.
type Server struct {
	opts ServerOptions
}

// NewServer creates an MCP server. Stub: returns 501 on tool calls until backend is wired.
func NewServer(opts ServerOptions) (*Server, error) {
	return &Server{opts: opts}, nil
}

// RunIndexer runs background indexing. Stub: no-op until ingestion pipeline exists.
func (s *Server) RunIndexer(ctx context.Context) {}

// MCPHandler returns the HTTP handler for the MCP path.
func (s *Server) MCPHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotImplemented)
		_, _ = w.Write([]byte("MCP server not implemented yet"))
	})
}

// Serve blocks while handling HTTP. Cancel ctx to initiate graceful shutdown.
func (s *Server) Serve(ctx context.Context, listener net.Listener) error {
	mux := http.NewServeMux()
	mux.Handle(s.opts.McpPath, s.MCPHandler())
	srv := &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	errCh := make(chan error, 1)
	go func() { errCh <- srv.Serve(listener) }()
	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return srv.Shutdown(shutdownCtx)
	case err := <-errCh:
		return err
	}
}
