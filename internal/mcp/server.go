package mcp

import (
	"context"
	"net/http"

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

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc(s.cfg.MCPPath, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotImplemented)
		_, _ = w.Write([]byte("mcp server skeleton: route not implemented"))
	})
	return mux
}

func (s *Server) Run(ctx context.Context) error {
	_ = ctx
	_ = s.retriever
	return model.ErrNotImplemented
}
