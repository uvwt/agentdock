package httpx

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"path/filepath"
	"time"

	"github.com/uvwt/agentdock/internal/auth"
	"github.com/uvwt/agentdock/internal/buildinfo"
	"github.com/uvwt/agentdock/internal/config"
	"github.com/uvwt/agentdock/internal/mcp"
	"github.com/uvwt/agentdock/internal/publicartifacts"
	"github.com/uvwt/agentdock/internal/runtimeapi"
)

func Serve(ctx context.Context, server *mcp.Server, runtime runtimeapi.Runtime, cfg config.Config) error {
	authRequired := cfg.AuthRequired()
	oauthStore := auth.NewOAuthStore()
	if cfg.OAuthEnabled {
		persistentStore, err := auth.NewPersistentOAuthStore(filepath.Join(cfg.AgentDockHome, "oauth", "state-v1.json"), oauthSigningKey())
		if err != nil {
			return fmt.Errorf("initialize OAuth token store: %w", err)
		}
		oauthStore = persistentStore
	}
	mux := http.NewServeMux()
	publicArtifactStore := publicartifacts.New(cfg.AgentDockHome, cfg.OAuthServerURL, cfg.Port)
	if err := publicArtifactStore.EnsureSecret(); err != nil {
		return fmt.Errorf("ensure public artifact secret: %w", err)
	}
	if err := publicArtifactStore.Cleanup(time.Now().UTC()); err != nil {
		return fmt.Errorf("clean public artifacts: %w", err)
	}
	slog.Info("http server configured", "host", cfg.Host, "port", cfg.Port, "auth_required", authRequired, "endpoint", "/mcp")
	mux.HandleFunc("/", statusPageHandler(server, cfg))
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("content-type", "application/json")
		writeJSON(w, map[string]any{"ok": true, "version": buildinfo.Version})
	})
	mux.HandleFunc("/.well-known/mcp.json", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, serverCard(cfg, r))
	})
	mux.HandleFunc("/.well-known/mcp/server-card.json", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, serverCard(cfg, r))
	})
	mux.HandleFunc("/artifacts/public/", func(w http.ResponseWriter, r *http.Request) {
		publicArtifactStore.ServeHTTP(w, r, "/artifacts/public/")
	})
	registerOAuthRoutes(mux, cfg, oauthStore)
	mux.HandleFunc("/context", agentDockContextHandler(server, cfg, oauthStore))
	registerRuntimeAPI(mux, runtime, cfg, oauthStore)
	mux.HandleFunc("/mcp", mcpEndpointHandler(server, cfg, oauthStore))

	addr := fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)
	httpServer := newHTTPServer(addr, loggingMiddleware(mux))
	slog.Info("http server listening", "addr", addr)
	return serveHTTP(ctx, httpServer)
}
func serveHTTP(ctx context.Context, server *http.Server) error {
	serveErr := make(chan error, 1)
	go func() { serveErr <- server.ListenAndServe() }()

	select {
	case err := <-serveErr:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		_ = server.Close()
		return fmt.Errorf("shutdown HTTP server: %w", err)
	}
	if err := <-serveErr; err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
func newHTTPServer(addr string, handler http.Handler) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       15 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}
}
