package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// ParameterType is a workflow parameter definition.
type ParameterType string

const (
	ParamString ParameterType = "string"
)

// Parameter defines a user-editable parameter in the workflow.
type Parameter struct {
	Type        ParameterType `json:"type"`
	Default     string        `json:"default,omitempty"`
	Description string        `json:"description,omitempty"`
}

// OutputType describes how a step's output should be rendered.
type OutputType string

const (
	OutputText    OutputType = "text"
	OutputMarkdown OutputType = "markdown"
)

// Step defines a single step in the workflow.
type Step struct {
	ID                 string     `json:"id"`
	Name               string     `json:"name"`
	Script             string     `json:"script"`
	Params             []string   `json:"params,omitempty"`
	RunOncePerSession  bool       `json:"run_once_per_session,omitempty"`
	AutoRun            bool       `json:"auto_run,omitempty"`
	Description        string     `json:"description,omitempty"`
	OutputType         OutputType `json:"output_type,omitempty"`
}

// Workflow is the top-level structure loaded from JSON.
type Workflow struct {
	Name        string                `json:"name"`
	Description string                `json:"description,omitempty"`
	Parameters  map[string]Parameter  `json:"parameters,omitempty"`
	Steps       []Step                `json:"steps"`
}

// LoadWorkflow reads and validates a workflow JSON file.
func LoadWorkflow(path string) (*Workflow, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading workflow file: %w", err)
	}

	var wf Workflow
	if err := json.Unmarshal(data, &wf); err != nil {
		return nil, fmt.Errorf("parsing workflow JSON: %w", err)
	}

	if err := wf.Validate(); err != nil {
		return nil, fmt.Errorf("validating workflow: %w", err)
	}

	return &wf, nil
}

// Validate checks the workflow for structural errors.
func (wf *Workflow) Validate() error {
	if wf.Name == "" {
		return fmt.Errorf("workflow name is required")
	}

	if len(wf.Steps) == 0 {
		return fmt.Errorf("workflow must have at least one step")
	}

	seenIDs := make(map[string]struct{})
	for i, step := range wf.Steps {
		if step.ID == "" {
			return fmt.Errorf("step %d: id is required", i)
		}
		if step.Name == "" {
			return fmt.Errorf("step %d: name is required", i)
		}
		if step.Script == "" {
			return fmt.Errorf("step %d: script is required", i)
		}
		if _, ok := seenIDs[step.ID]; ok {
			return fmt.Errorf("step %d: duplicate id %q", i, step.ID)
		}
		seenIDs[step.ID] = struct{}{}

		if step.OutputType != "" && step.OutputType != OutputText && step.OutputType != OutputMarkdown {
			return fmt.Errorf("step %d: unknown output_type %q", i, step.OutputType)
		}

		seenParams := make(map[string]struct{})
		for _, param := range step.Params {
			if _, ok := seenParams[param]; ok {
				return fmt.Errorf("step %q: duplicate parameter %q", step.ID, param)
			}
			seenParams[param] = struct{}{}
			if _, ok := wf.Parameters[param]; !ok {
				return fmt.Errorf("step %q references undefined parameter %q", step.ID, param)
			}
		}
	}

	return nil
}

// ResolveScriptPath resolves the script path relative to the workflow file.
func ResolveScriptPath(workflowDir, script string) string {
	if script == "" {
		return ""
	}
	if filepath.IsAbs(script) {
		return script
	}
	return filepath.Join(workflowDir, script)
}
