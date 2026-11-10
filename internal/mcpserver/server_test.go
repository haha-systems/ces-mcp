package mcpserver

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/haha-systems/ces-mcp/internal/engine"
	"github.com/haha-systems/ces-mcp/internal/httptransport"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestMCPProtocolToolsResourcesAndSchemaValidation(t *testing.T) {
	ctx := context.Background()

	manager, err := engine.NewManager(t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}

	server := New(manager)
	client := mcp.NewClient(&mcp.Implementation{Name: "ces-mcp-test-client", Version: "0.2.0"}, nil)
	serverTransport, clientTransport := mcp.NewInMemoryTransports()

	serverSession, err := server.MCP().Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer serverSession.Close()

	clientSession, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer clientSession.Close()

	listed, err := clientSession.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}

	if len(listed.Tools) != 11 {
		t.Fatalf("MCP tools = %d, want 11", len(listed.Tools))
	}

	seen := map[string]bool{}
	for _, tool := range listed.Tools {
		seen[tool.Name] = true
		if tool.InputSchema == nil || tool.OutputSchema == nil {
			t.Fatalf("tool %s has no input/output schema", tool.Name)
		}
	}

	for _, name := range []string{"ces_create", "ces_status", "ces_project", "ces_dispatch", "ces_submit", "ces_validate", "ces_accept", "ces_reject", "ces_advance", "ces_history", "ces_export"} {
		if !seen[name] {
			t.Fatalf("MCP server did not advertise %s", name)
		}
	}

	created, err := clientSession.CallTool(
		ctx,
		&mcp.CallToolParams{
			Name:      "ces_create",
			Arguments: map[string]any{"goal": "inspect MCP setup"},
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	if created.IsError {
		t.Fatalf("ces_create returned error: %+v", created.StructuredContent)
	}

	createdBody := responseBody(t, created)
	data := createdBody["data"].(map[string]any)
	runID := data["run_id"].(string)

	invalid, err := clientSession.CallTool(
		ctx,
		&mcp.CallToolParams{Name: "ces_create", Arguments: map[string]any{"unknown": true}},
	)
	if err != nil {
		t.Fatal(err)
	}

	if !invalid.IsError {
		t.Fatal("invalid tool arguments were not marked as an MCP tool error")
	}

	invalidBody := responseBody(t, invalid)

	toolError := invalidBody["error"].(map[string]any)
	if toolError["code"] != "invalid_arguments" || invalidBody["ok"] != false {
		t.Fatalf("invalid arguments had no structured error code: %+v", invalidBody)
	}

	phaseResource, err := clientSession.ReadResource(
		ctx,
		&mcp.ReadResourceParams{URI: "ces://phase/triage"},
	)
	if err != nil {
		t.Fatal(err)
	}

	if len(phaseResource.Contents) != 1 ||
		!strings.Contains(phaseResource.Contents[0].Text, "output_schema") ||
		!strings.Contains(phaseResource.Contents[0].Text, "TRIAGE") {
		t.Fatalf("phase resource is incomplete: %+v", phaseResource)
	}

	specResource, err := clientSession.ReadResource(
		ctx,
		&mcp.ReadResourceParams{URI: "ces://spec/v0.2.0"},
	)
	if err != nil {
		t.Fatal(err)
	}

	if len(specResource.Contents) != 1 ||
		!strings.Contains(specResource.Contents[0].Text, "dispatch_reference") {
		t.Fatalf("schema resource is incomplete: %+v", specResource)
	}

	projectionResource, err := clientSession.ReadResource(
		ctx,
		&mcp.ReadResourceParams{URI: "ces://runs/" + runID + "/projection"},
	)
	if err != nil {
		t.Fatal(err)
	}

	var projection map[string]any
	if err := json.Unmarshal([]byte(projectionResource.Contents[0].Text), &projection); err != nil {
		t.Fatal(err)
	}

	if projection["state_version"] != float64(0) {
		t.Fatalf("unexpected resource projection: %+v", projection)
	}
}

func TestStreamableHTTPTransportSupportsMCPClient(t *testing.T) {
	ctx := context.Background()
	manager, err := engine.NewManager(t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	server := New(manager)
	handler, err := httptransport.New(server.StreamableHTTPHandler(), httptransport.Config{
		Address: "127.0.0.1:8080", Path: "/mcp",
	})
	if err != nil {
		t.Fatal(err)
	}
	httpServer := httptest.NewServer(handler.Handler)
	defer httpServer.Close()

	client := mcp.NewClient(&mcp.Implementation{Name: "ces-http-test-client", Version: "0.2.0"}, nil)
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: httpServer.URL + "/mcp"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()

	tools, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(tools.Tools) != 11 {
		t.Fatalf("Streamable HTTP tools = %d, want 11", len(tools.Tools))
	}
	created, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name: "ces_create", Arguments: map[string]any{"goal": "remote HTTP run"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.IsError || responseBody(t, created)["ok"] != true {
		t.Fatalf("HTTP ces_create failed: %+v", created.StructuredContent)
	}
}

func TestStructuredResponseSchemaValidation(t *testing.T) {
	result := responseResult(
		ToolResponse{OK: true, Data: map[string]any{"run_id": "ces_example"}},
		false,
	)
	if result.IsError || result.StructuredContent == nil || len(result.Content) != 1 {
		t.Fatalf("valid structured response was rejected: %+v", result)
	}

	if failures := engine.ValidateJSONDocument(
		[]byte(`{"goal":1}`),
		createSchema(),
	); len(
		failures,
	) == 0 {
		t.Fatal("input schema accepted a number where a string is required")
	}

	if failures := engine.ValidateJSONDocument(
		[]byte(`{"goal":"a","goal":"b"}`),
		createSchema(),
	); len(
		failures,
	) == 0 {
		t.Fatal("input schema accepted duplicate JSON keys")
	}
}

func responseBody(t *testing.T, result *mcp.CallToolResult) map[string]any {
	t.Helper()

	body, ok := result.StructuredContent.(map[string]any)
	if !ok {
		t.Fatalf("structured content type is %T", result.StructuredContent)
	}

	return body
}
