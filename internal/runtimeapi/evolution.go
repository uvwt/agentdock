package runtimeapi

import (
	"bytes"
	"encoding/json"
	"io"

	"github.com/uvwt/agentdock/internal/app"
)

type evolutionCandidate struct {
	Type         string   `json:"type"`
	Statement    string   `json:"statement"`
	Scope        string   `json:"scope,omitempty"`
	Project      string   `json:"project,omitempty"`
	Device       string   `json:"device,omitempty"`
	CanonicalKey string   `json:"canonical_key,omitempty"`
	Source       string   `json:"source,omitempty"`
	Tags         []string `json:"tags,omitempty"`
}

type evolutionRequest struct {
	Intent         string              `json:"intent"`
	Candidate      *evolutionCandidate `json:"candidate,omitempty"`
	EvolutionID    string              `json:"evolution_id,omitempty"`
	TaskID         string              `json:"task_id,omitempty"`
	ReviewRevision string              `json:"review_revision,omitempty"`
	Relation       string              `json:"relation,omitempty"`
	EvidenceRefs   []string            `json:"evidence_refs,omitempty"`
	Rationale      string              `json:"rationale,omitempty"`
	SupersededBy   string              `json:"superseded_by,omitempty"`
}

func decodeRuntimeEvolutionRequest(body []byte) (map[string]any, error) {
	if len(body) > 64*1024 {
		return nil, evolutionRequestError("evolve request body is too large")
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	var request evolutionRequest
	if err := decoder.Decode(&request); err != nil {
		return nil, evolutionRequestError("invalid evolve request body")
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return nil, evolutionRequestError("request body must contain exactly one JSON value")
	}
	data, err := json.Marshal(request)
	if err != nil {
		return nil, evolutionRequestError("failed to normalize evolve request")
	}
	var args map[string]any
	if err := json.Unmarshal(data, &args); err != nil {
		return nil, evolutionRequestError("failed to normalize evolve request")
	}
	return args, nil
}

func evolutionRequestError(message string) error {
	return &app.ToolError{Code: "INVALID_EVOLVE_REQUEST", Message: message, Category: "validation"}
}
