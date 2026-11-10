package mcpserver

import (
	"context"
	"encoding/json"

	"github.com/haha-systems/ces-mcp/internal/engine"
	"github.com/haha-systems/ces-mcp/internal/epistemic"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func (s *Server) registerResources() {
	s.mcp.AddResource(&mcp.Resource{
		URI:         "ces://spec/v0.2.0",
		Name:        "CES v0.2.0 schema and protocol",
		MIMEType:    "application/schema+json",
		Description: "The CES record, dispatch, submission, and projection envelope schema.",
	}, func(context.Context, *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
		return resourceJSON("ces://spec/v0.2.0", schemaResource()), nil
	})

	for _, phase := range []epistemic.Phase{
		epistemic.PhaseTriage, epistemic.PhaseAbduce, epistemic.PhaseFrame,
		epistemic.PhaseExecute, epistemic.PhaseClose,
	} {
		phase := phase
		uri := "ces://phase/" + string(phase)
		s.mcp.AddResource(&mcp.Resource{
			URI: uri, Name: "CES " + string(phase) + " contract", MIMEType: "application/json",
			Description: "Phase instructions and the v0.2.0 structured output schema.",
		}, func(context.Context, *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
			return &mcp.ReadResourceResult{
				Contents: []*mcp.ResourceContents{
					{URI: uri, MIMEType: "application/json", Text: phaseResourceText(phase)},
				},
			}, nil
		})
	}

	s.mcp.AddResourceTemplate(&mcp.ResourceTemplate{
		URITemplate: "ces://runs/{run_id}/{view}",
		Name:        "CES run view",
		MIMEType:    "application/json",
		Description: "Read an authorised current projection, accepted event history, or complete run export.",
	}, func(ctx context.Context, request *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
		runID, view, err := parseRunResourceURI(request.Params.URI)
		if err != nil {
			return nil, mcp.ResourceNotFoundError(request.Params.URI)
		}

		var value any

		switch view {
		case "projection":
			value, err = s.manager.Project(ctx, runID)
		case "history":
			value, err = s.manager.History(
				ctx,
				runID,
				parseIntQuery(request.Params.URI, "offset", 0),
				parseIntQuery(request.Params.URI, "limit", 100),
			)
		case "export":
			value, err = s.manager.Export(ctx, runID)
		default:
			return nil, mcp.ResourceNotFoundError(request.Params.URI)
		}

		if err != nil {
			return nil, mcp.ResourceNotFoundError(request.Params.URI)
		}

		return resourceJSON(request.Params.URI, value), nil
	})
}

func resourceJSON(uri string, value any) *mcp.ReadResourceResult {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		data = []byte(
			`{"error":{"code":"serialization_failed","message":"could not encode resource"}}`,
		)
	}

	return &mcp.ReadResourceResult{
		Contents: []*mcp.ResourceContents{
			{URI: uri, MIMEType: "application/json", Text: string(data)},
		},
	}
}

func schemaResource() map[string]any {
	return map[string]any{
		"$schema": "https://json-schema.org/draft/2020-12/schema",
		"$id":     "ces://spec/v0.2.0",
		"title":   "Cumulative Epistemic State v0.2.0",
		"type":    "object",
		"properties": map[string]any{
			"ces_version": map[string]any{"const": engine.CESVersion},
			"phases": map[string]any{
				"const": []string{"triage", "abduce", "frame", "execute", "close"},
			},
			"record_kinds": map[string]any{"const": []string{
				"observation",
				"claim",
				"hypothesis",
				"unknown",
				"constraint",
				"frame",
				"action",
				"outcome",
				"relation",
			}},
			"dispatch_reference": strictObject(
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
					"dispatch_id": stringSchema("Original dispatch identifier."),
					"run_id":      stringSchema("CES run identifier."),
					"phase": enumSchema(
						[]any{"triage", "abduce", "frame", "execute", "close"},
						"Phase identifier.",
					),
					"state_version":   integerSchema("State version used for dispatch."),
					"state_hash":      stringSchema("Accepted state hash."),
					"projection_hash": stringSchema("Authorised projection hash."),
					"prompt_hash":     stringSchema("Phase instructions and output schema hash."),
					"session_id":      stringSchema("Agent session identifier."),
					"parent_run_id":   stringSchema("Optional parent run identifier."),
				},
			),
			"artifact_envelope": strictObject([]string{"dispatch", "result"}, map[string]any{
				"dispatch": map[string]any{"$ref": "#/properties/dispatch_reference"},
				"result": map[string]any{
					"type":        "object",
					"description": "One of the phase output schemas at ces://phase/{phase}.",
				},
			}),
			"projection": map[string]any{
				"type": "object",
				"required": []string{
					"schema_version",
					"run_id",
					"state_version",
					"state_hash",
					"phase",
					"state",
					"ids",
					"evidence",
					"accepted_decisions",
				},
				"properties": map[string]any{
					"schema_version": map[string]any{"const": engine.CESVersion},
					"run_id": stringSchema(
						"CES run identifier.",
					),
					"state_version": integerSchema("Accepted state version."),
					"state_hash":    stringSchema("Hash of accepted events."),
					"phase": enumSchema(
						[]any{"triage", "abduce", "frame", "execute", "close"},
						"Current phase.",
					),
					"state": map[string]any{
						"type": "object",
					},
					"ids": arraySchema(stringSchema("Projection object identifier.")),
					"evidence": map[string]any{
						"type":  "array",
						"items": map[string]any{"type": "object"},
					},
					"accepted_decisions": map[string]any{
						"type":  "array",
						"items": map[string]any{"type": "object"},
					},
				},
				"additionalProperties": false,
			},
		},
		"required": []string{
			"ces_version",
			"phases",
			"record_kinds",
			"dispatch_reference",
			"artifact_envelope",
			"projection",
		},
		"additionalProperties": false,
	}
}
