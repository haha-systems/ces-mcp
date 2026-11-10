package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/haha-systems/ces-mcp/internal/engine"
	"github.com/haha-systems/ces-mcp/internal/mcpserver"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	dataDir := os.Getenv("CES_DATA_DIR")

	runtime, err := engine.NewCommandRuntime(
		os.Getenv("CES_RUNTIME_EXECUTABLE"),
		os.Getenv("CES_RUNTIME_ARGS"),
		os.Getenv("CES_RUNTIME_ENV_ALLOWLIST"),
	)
	if err != nil {
		logger.Error("configure CES runtime", "error", err)
		os.Exit(1)
	}

	manager, err := engine.NewManager(dataDir, runtime)
	if err != nil {
		logger.Error("configure CES storage", "error", err)
		os.Exit(1)
	}

	server := mcpserver.New(manager)
	if err := server.Run(context.Background(), &mcp.StdioTransport{}); err != nil {
		fmt.Fprintln(os.Stderr, "CES MCP server stopped:", err)
		os.Exit(1)
	}
}
