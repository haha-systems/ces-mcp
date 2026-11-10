package mcpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/haha-systems/ces-mcp/internal/cognition"
	"github.com/haha-systems/ces-mcp/internal/engine"
	"github.com/haha-systems/ces-mcp/internal/epistemic"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type Server struct {
	manager *engine.Manager
	mcp     *mcp.Server
}

type ToolError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type ToolResponse struct {
	OK    bool       `json:"ok"`
	Data  any        `json:"data,omitempty"`
	Error *ToolError `json:"error,omitempty"`
}

type createInput struct {
	Goal        string `json:"goal"`
	ParentRunID string `json:"parent_run_id,omitempty"`
}

type runInput struct {
	RunID string `json:"run_id"`
}

type dispatchInput struct {
	RunID        string   `json:"run_id"`
	StateVersion uint64   `json:"state_version"`
	Mode         string   `json:"mode"`
	SessionID    string   `json:"session_id,omitempty"`
	AllowedTools []string `json:"allowed_tools,omitempty"`
}

type dispatchReceiptInput struct {
	DispatchID     string          `json:"dispatch_id"`
	RunID          string          `json:"run_id"`
	Phase          epistemic.Phase `json:"phase"`
	StateVersion   uint64          `json:"state_version"`
	StateHash      string          `json:"state_hash"`
	ProjectionHash string          `json:"projection_hash"`
	PromptHash     string          `json:"prompt_hash"`
	SessionID      string          `json:"session_id"`
	ParentRunID    string          `json:"parent_run_id,omitempty"`
}

type artifactEnvelopeInput struct {
	Dispatch dispatchReceiptInput `json:"dispatch"`
	Result   map[string]any       `json:"result"`
}

type submitInput struct {
	RunID          string                `json:"run_id"`
	IdempotencyKey string                `json:"idempotency_key"`
	Artifact       artifactEnvelopeInput `json:"artifact"`
}

type proposalInput struct {
	RunID      string `json:"run_id"`
	ProposalID string `json:"proposal_id"`
}

type acceptInput struct {
	RunID          string `json:"run_id"`
	ProposalID     string `json:"proposal_id"`
	StateVersion   uint64 `json:"state_version"`
	IdempotencyKey string `json:"idempotency_key"`
}

type rejectInput struct {
	RunID      string `json:"run_id"`
	ProposalID string `json:"proposal_id"`
	Reason     string `json:"reason"`
}

type advanceInput struct {
	RunID        string `json:"run_id"`
	StateVersion uint64 `json:"state_version"`
}

type historyInput struct {
	RunID  string `json:"run_id"`
	Offset int    `json:"offset,omitempty"`
	Limit  int    `json:"limit,omitempty"`
}

func New(manager *engine.Manager) *Server {
	server := mcp.NewServer(
		&mcp.Implementation{Name: "ces-mcp", Version: "0.2.0"},
		&mcp.ServerOptions{
			Instructions: "CES owns persistent epistemic state. Agents propose phase artifacts; the CES state engine validates and commits them.",
		},
	)
	s := &Server{manager: manager, mcp: server}
	s.registerTools()
	s.registerResources()

	return s
}

func (s *Server) Run(ctx context.Context, transport mcp.Transport) error {
	return s.mcp.Run(ctx, transport)
}

func (s *Server) MCP() *mcp.Server { return s.mcp }

func (s *Server) registerTools() {
	addTool(
		s.mcp,
		"ces_create",
		"Create a persistent CES run.",
		createSchema(),
		func(ctx context.Context, in createInput) (any, error) {
			return s.manager.Create(ctx, in.Goal, in.ParentRunID)
		},
	)
	addTool(
		s.mcp,
		"ces_status",
		"Inspect the current CES phase, state version, hash, and pending work.",
		runSchema(),
		func(ctx context.Context, in runInput) (any, error) {
			return s.manager.Status(ctx, in.RunID)
		},
	)
	addTool(
		s.mcp,
		"ces_project",
		"Generate the authorised projection and phase contract for the current state.",
		runSchema(),
		func(ctx context.Context, in runInput) (any, error) {
			return s.manager.Project(ctx, in.RunID)
		},
	)
	addTool(
		s.mcp,
		"ces_dispatch",
		"Prepare a hash-bound phase dispatch for a host-managed or configured server-managed agent.",
		dispatchSchema(),
		func(ctx context.Context, in dispatchInput) (any, error) {
			return s.manager.Dispatch(ctx, engine.DispatchRequest{
				RunID: in.RunID, StateVersion: in.StateVersion, SessionID: in.SessionID,
				Mode: in.Mode, AllowedTools: in.AllowedTools,
			})
		},
	)
	addTool(
		s.mcp,
		"ces_submit",
		"Submit a structured phase result that references its original dispatch.",
		submitSchema(),
		func(ctx context.Context, in submitInput) (any, error) {
			resultJSON, err := json.Marshal(in.Artifact.Result)
			if err != nil {
				return nil, fmt.Errorf("encode phase result: %w", err)
			}

			receipt := engine.DispatchReference{
				DispatchID:     in.Artifact.Dispatch.DispatchID,
				RunID:          in.Artifact.Dispatch.RunID,
				Phase:          in.Artifact.Dispatch.Phase,
				StateVersion:   in.Artifact.Dispatch.StateVersion,
				StateHash:      in.Artifact.Dispatch.StateHash,
				ProjectionHash: in.Artifact.Dispatch.ProjectionHash,
				PromptHash:     in.Artifact.Dispatch.PromptHash,
				SessionID:      in.Artifact.Dispatch.SessionID,
				ParentRunID:    in.Artifact.Dispatch.ParentRunID,
			}

			return s.manager.Submit(ctx, engine.SubmitRequest{
				RunID: in.RunID, IdempotencyKey: in.IdempotencyKey, Dispatch: receipt,
				Result: resultJSON,
			})
		},
	)
	addTool(
		s.mcp,
		"ces_validate",
		"Validate a submitted proposal against the current projection and phase contract.",
		proposalSchema(),
		func(ctx context.Context, in proposalInput) (any, error) {
			return s.manager.Validate(ctx, in.RunID, in.ProposalID)
		},
	)
	addTool(
		s.mcp,
		"ces_accept",
		"Commit a validated proposal using compare-and-swap state versioning.",
		acceptSchema(),
		func(ctx context.Context, in acceptInput) (any, error) {
			return s.manager.Accept(
				ctx,
				in.RunID,
				in.ProposalID,
				in.StateVersion,
				in.IdempotencyKey,
			)
		},
	)
	addTool(
		s.mcp,
		"ces_reject",
		"Record a rejected proposal without changing accepted CES state.",
		rejectSchema(),
		func(ctx context.Context, in rejectInput) (any, error) {
			return s.manager.Reject(ctx, in.RunID, in.ProposalID, in.Reason)
		},
	)
	addTool(
		s.mcp,
		"ces_advance",
		"Advance exactly one permitted phase after its proposal is accepted.",
		advanceSchema(),
		func(ctx context.Context, in advanceInput) (any, error) {
			return s.manager.Advance(ctx, in.RunID, in.StateVersion)
		},
	)
	addTool(
		s.mcp,
		"ces_history",
		"Inspect accepted events, decisions, dispatches, submissions, and rejected attempts.",
		historySchema(),
		func(ctx context.Context, in historyInput) (any, error) {
			limit := in.Limit
			if limit == 0 {
				limit = 100
			}

			return s.manager.History(ctx, in.RunID, in.Offset, limit)
		},
	)
	addTool(
		s.mcp,
		"ces_export",
		"Export a complete run with its event history and replayable accepted state.",
		runSchema(),
		func(ctx context.Context, in runInput) (any, error) {
			return s.manager.Export(ctx, in.RunID)
		},
	)
}

func addTool[In any](
	server *mcp.Server,
	name, description string,
	schema map[string]any,
	handler func(context.Context, In) (any, error),
) {
	tool := &mcp.Tool{
		Name:         name,
		Description:  description,
		InputSchema:  schema,
		OutputSchema: responseSchema(),
	}
	server.AddTool(
		tool,
		func(ctx context.Context, request *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			if failures := engine.ValidateJSONDocument(
				request.Params.Arguments,
				schema,
			); len(
				failures,
			) > 0 {
				return responseResult(
					ToolResponse{
						OK: false,
						Error: &ToolError{
							Code:    "invalid_arguments",
							Message: strings.Join(failures, "; "),
						},
					},
					true,
				), nil
			}

			var input In

			strict := json.NewDecoder(bytes.NewReader(request.Params.Arguments))
			strict.DisallowUnknownFields()

			if err := strict.Decode(&input); err != nil {
				return responseResult(
					ToolResponse{
						OK:    false,
						Error: &ToolError{Code: "invalid_arguments", Message: err.Error()},
					},
					true,
				), nil
			}

			data, err := handler(ctx, input)
			if err != nil {
				response := ToolResponse{
					OK:    false,
					Data:  data,
					Error: &ToolError{Code: engine.ErrorCode(err), Message: err.Error()},
				}

				return responseResult(response, true), nil
			}

			return responseResult(ToolResponse{OK: true, Data: data}, false), nil
		},
	)
}

func responseResult(response ToolResponse, isError bool) *mcp.CallToolResult {
	encoded, err := json.Marshal(response)
	if err != nil {
		response = ToolResponse{
			OK:    false,
			Error: &ToolError{Code: "internal_error", Message: "could not encode tool response"},
		}
		encoded, _ = json.Marshal(response)
		isError = true
	}

	if failures := engine.ValidateJSONDocument(encoded, responseSchema()); len(failures) > 0 {
		response = ToolResponse{
			OK: false,
			Error: &ToolError{
				Code: "internal_error",
				Message: "tool response did not match its output schema: " + strings.Join(
					failures,
					"; ",
				),
			},
		}
		encoded, _ = json.Marshal(response)
		isError = true
	}

	return &mcp.CallToolResult{
		Content:           []mcp.Content{&mcp.TextContent{Text: string(encoded)}},
		StructuredContent: response,
		IsError:           isError,
	}
}

func createSchema() map[string]any {
	return strictObject([]string{"goal"}, map[string]any{
		"goal":          stringSchema("Problem or objective for this CES run."),
		"parent_run_id": stringSchema("Optional parent CES run identifier."),
	})
}
func runSchema() map[string]any {
	return strictObject(
		[]string{"run_id"},
		map[string]any{"run_id": stringSchema("CES run identifier.")},
	)
}
func dispatchSchema() map[string]any {
	return strictObject([]string{"run_id", "state_version", "mode"}, map[string]any{
		"run_id": stringSchema(
			"CES run identifier.",
		),
		"state_version": integerSchema("Expected current state version."),
		"mode":          enumSchema([]any{"host_managed", "server_managed"}, "Execution mode."),
		"session_id":    stringSchema("Host session identifier. Required in host_managed mode."),
		"allowed_tools": arraySchema(stringSchema("Tool available to the phase agent.")),
	})
}
func submitSchema() map[string]any {
	return strictObject([]string{"run_id", "idempotency_key", "artifact"}, map[string]any{
		"run_id": stringSchema(
			"CES run identifier.",
		),
		"idempotency_key": stringSchema("Stable key for retrying this submission."),
		"artifact": strictObject([]string{"dispatch", "result"}, map[string]any{
			"dispatch": strictObject(
				[]string{
					"dispatch_id",
					"run_id",
					"phase",
					"state_version",
					"state_hash",
					"projection_hash",
					"prompt_hash",
					"session_id",
				},
				map[string]any{
					"dispatch_id": stringSchema(
						"Original dispatch identifier.",
					),
					"run_id": stringSchema("Run identifier from the dispatch."),
					"phase": enumSchema(
						[]any{"triage", "abduce", "frame", "execute", "close"},
						"Dispatched phase.",
					),
					"state_version": integerSchema("State version in the original dispatch."),
					"state_hash":    stringSchema("Accepted state hash in the dispatch."),
					"projection_hash": stringSchema(
						"Authorised projection hash.",
					),
					"prompt_hash": stringSchema("Phase instruction and schema hash."),
					"session_id": stringSchema(
						"Agent session identifier.",
					),
					"parent_run_id": stringSchema("Optional parent CES run identifier."),
				},
			),
			"result": map[string]any{
				"type":        "object",
				"description": "Phase-specific structured result. Read the phase schema from ces_project.",
			},
		}),
	})
}
func proposalSchema() map[string]any {
	return strictObject(
		[]string{"run_id", "proposal_id"},
		map[string]any{
			"run_id":      stringSchema("CES run identifier."),
			"proposal_id": stringSchema("Submitted proposal identifier."),
		},
	)
}
func acceptSchema() map[string]any {
	return strictObject(
		[]string{"run_id", "proposal_id", "state_version", "idempotency_key"},
		map[string]any{
			"run_id": stringSchema(
				"CES run identifier.",
			),
			"proposal_id": stringSchema("Validated proposal identifier."),
			"state_version": integerSchema(
				"Expected current state version.",
			),
			"idempotency_key": stringSchema("Stable key for retrying this acceptance."),
		},
	)
}
func rejectSchema() map[string]any {
	return strictObject([]string{"run_id", "proposal_id", "reason"}, map[string]any{
		"run_id": stringSchema(
			"CES run identifier.",
		),
		"proposal_id": stringSchema("Proposal identifier."),
		"reason":      stringSchema("Reason for rejection."),
	})
}
func advanceSchema() map[string]any {
	return strictObject(
		[]string{"run_id", "state_version"},
		map[string]any{
			"run_id":        stringSchema("CES run identifier."),
			"state_version": integerSchema("Expected current state version."),
		},
	)
}
func historySchema() map[string]any {
	return strictObject([]string{"run_id"}, map[string]any{
		"run_id": stringSchema(
			"CES run identifier.",
		),
		"offset": integerSchema("Event offset."),
		"limit":  integerSchema("Maximum number of accepted events; defaults to 100."),
	})
}

func strictObject(required []string, properties map[string]any) map[string]any {
	return map[string]any{
		"type":                 "object",
		"properties":           properties,
		"required":             required,
		"additionalProperties": false,
	}
}
func stringSchema(description string) map[string]any {
	return map[string]any{"type": "string", "description": description}
}
func integerSchema(description string) map[string]any {
	return map[string]any{"type": "integer", "minimum": 0, "description": description}
}
func enumSchema(values []any, description string) map[string]any {
	return map[string]any{"type": "string", "enum": values, "description": description}
}
func arraySchema(item map[string]any) map[string]any {
	return map[string]any{"type": "array", "items": item}
}

func responseSchema() map[string]any {
	return map[string]any{
		"type": "object", "required": []string{"ok"}, "additionalProperties": false,
		"properties": map[string]any{
			"ok":   map[string]any{"type": "boolean"},
			"data": map[string]any{"type": "object"},
			"error": strictObject(
				[]string{"code", "message"},
				map[string]any{
					"code":    stringSchema("Machine-readable error code."),
					"message": stringSchema("Error description."),
				},
			),
		},
	}
}

func phaseResourceText(phase epistemic.Phase) string {
	contract, _ := cognition.Contract(phase)
	schema := cognition.PhaseOutputSchema(phase, epistemic.Projection{Phase: phase})
	data, _ := json.MarshalIndent(
		map[string]any{
			"ces_version":   engine.CESVersion,
			"phase":         phase,
			"instructions":  contract.String(),
			"output_schema": schema,
		},
		"",
		"  ",
	)

	return string(data)
}

func parseRunResourceURI(uri string) (string, string, error) {
	parsed, err := url.Parse(uri)
	if err != nil || parsed.Scheme != "ces" || parsed.Host != "runs" {
		return "", "", fmt.Errorf("invalid CES resource URI")
	}

	parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	if len(parts) != 2 {
		return "", "", fmt.Errorf("invalid CES resource path")
	}

	runID, err := url.PathUnescape(parts[0])
	if err != nil {
		return "", "", fmt.Errorf("invalid CES run identifier")
	}

	return runID, parts[1], nil
}

func parseIntQuery(uri, key string, fallback int) int {
	parsed, err := url.Parse(uri)
	if err != nil {
		return fallback
	}

	value, err := strconv.Atoi(parsed.Query().Get(key))
	if err != nil || value < 0 {
		return fallback
	}

	return value
}
