package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	charmLog "charm.land/log/v2"
	"github.com/haha-systems/ces-mcp/internal/engine"
	"github.com/haha-systems/ces-mcp/internal/httptransport"
	"github.com/haha-systems/ces-mcp/internal/mcpserver"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func main() {
	logger := slog.New(charmLog.NewWithOptions(os.Stderr, charmLog.Options{
		Level:           charmLog.DebugLevel,
		Prefix:          "ces-mcp",
		ReportTimestamp: true,
		ReportCaller:    true,
	}))

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	runtime, err := engine.NewCommandRuntime(
		os.Getenv("CES_RUNTIME_EXECUTABLE"),
		os.Getenv("CES_RUNTIME_ARGS"),
		os.Getenv("CES_RUNTIME_ENV_ALLOWLIST"),
	)
	if err != nil {
		logger.Error("configure CES runtime", "error", err)
		os.Exit(1)
	}

	manager, err := engine.NewManager(os.Getenv("CES_DATA_DIR"), runtime)
	if err != nil {
		logger.Error("configure CES storage", "error", err)
		os.Exit(1)
	}

	server := mcpserver.NewWithLogger(manager, logger)

	var runErr error

	switch strings.ToLower(strings.TrimSpace(os.Getenv("CES_TRANSPORT"))) {
	case "", "stdio":
		runErr = server.Run(ctx, &mcp.StdioTransport{})
	case "http", "streamable-http":
		logger.Info(
			"Starting Streamable HTTP server",
		)
		runErr = serveHTTP(ctx, logger, server)
	default:
		runErr = fmt.Errorf("CES_TRANSPORT must be stdio or http")
	}

	if runErr != nil {
		logger.Error("CES MCP server stopped", "error", runErr)
		os.Exit(1)
	}
}

func serveHTTP(ctx context.Context, logger *slog.Logger, server *mcpserver.Server) error {
	address := os.Getenv("CES_HTTP_ADDR")
	path := os.Getenv("CES_HTTP_PATH")
	certFile := os.Getenv("CES_HTTP_TLS_CERT_FILE")
	keyFile := os.Getenv("CES_HTTP_TLS_KEY_FILE")
	mcpHandler := server.StreamableHTTPHandler()

	httpServer, err := httptransport.New(mcpHandler, httptransport.Config{
		Address: address, Path: path, BearerToken: os.Getenv("CES_HTTP_BEARER_TOKEN"),
		TLSCertFile: certFile, TLSKeyFile: keyFile,
	})
	if err != nil {
		return err
	}

	logger.Info(
		"CES MCP Streamable HTTP endpoint configured",
		"address",
		httpServer.Addr,
		"path",
		defaultPath(path),
	)

	serveResult := make(chan error, 1)

	go func() {
		if certFile != "" {
			serveResult <- httpServer.ListenAndServeTLS(certFile, keyFile)
			return
		}

		serveResult <- httpServer.ListenAndServe()
	}()

	select {
	case err := <-serveResult:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}

		return err
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		if err := httpServer.Shutdown(shutdownCtx); err != nil {
			closeErr := httpServer.Close()
			if closeErr != nil {
				return fmt.Errorf("shut down HTTP server: %v; force close: %w", err, closeErr)
			}

			return fmt.Errorf("shut down HTTP server: %w", err)
		}

		if err := <-serveResult; err != nil && !errors.Is(err, http.ErrServerClosed) {
			return err
		}

		return nil
	}
}

func defaultPath(path string) string {
	if path == "" {
		return "/mcp"
	}

	return path
}
