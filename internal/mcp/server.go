package mcp

import (
	"context"
	"net"
	"net/http"
	"time"

	"dir2mcp/internal/config"
)

// ServerOptions for running the MCP server.
type ServerOptions struct {
	RootDir   string
	StateDir  string
	Config    *config.Config
	McpPath   string
	AuthToken string
}

// Server is a minimal stub until full MCP Streamable HTTP server is integrated (main has a full implementation with sessions/auth).
type Server struct {
	opts ServerOptions
}

// NewServer creates an MCP server (stub: used by CLI up for shared mux with /api/mcp proxy).
func NewServer(opts ServerOptions) (*Server, error) {
	return &Server{opts: opts}, nil
}

// RunIndexer runs background indexing (stub: no-op until pipeline exists).
func (s *Server) RunIndexer(ctx context.Context) {}

// MCPHandler returns the HTTP handler for the MCP path (for mounting on a shared mux).
func (s *Server) MCPHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotImplemented)
		_, _ = w.Write([]byte("MCP server not implemented yet"))
	})
}

// Serve blocks while handling HTTP. Stub: responds 501 on the MCP path.
// Cancel ctx to initiate graceful shutdown; in-flight requests are allowed to drain.
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
