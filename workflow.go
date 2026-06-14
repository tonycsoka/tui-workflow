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
	OutputText     OutputType = "text"
	OutputMarkdown OutputType = "markdown"
)

// Step defines a single step in the workflow.
type Step struct {
	ID                string     `json:"id"`
	Name              string     `json:"name"`
	Script            string     `json:"script"`
	Params            []string   `json:"params,omitempty"`
	RunOnce bool       `json:"run_once,omitempty"`
	AutoRun           bool       `json:"auto_run,omitempty"`
	Description       string     `json:"description,omitempty"`
	OutputType        OutputType `json:"output_type,omitempty"`
}

// ParallelGroup defines a group of steps that run in parallel.
type ParallelGroup struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Steps       []Step `json:"steps"`
}

// StepOrGroup is a union type that can hold either a Step or a ParallelGroup.
type StepOrGroup struct {
	Step  *Step
	Group *ParallelGroup
}

// UnmarshalJSON implements custom JSON unmarshaling for backward compatibility.
// If the JSON object contains a "steps" key, it unmarshals as a ParallelGroup.
// Otherwise, it unmarshals as a Step.
func (s *StepOrGroup) UnmarshalJSON(data []byte) error {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	if _, ok := raw["steps"]; ok {
		var group ParallelGroup
		if err := json.Unmarshal(data, &group); err != nil {
			return err
		}
		s.Group = &group
		return nil
	}
	var step Step
	if err := json.Unmarshal(data, &step); err != nil {
		return err
	}
	s.Step = &step
	return nil
}

// MarshalJSON implements custom JSON marshaling.
func (s StepOrGroup) MarshalJSON() ([]byte, error) {
	if s.Step != nil {
		return json.Marshal(s.Step)
	}
	if s.Group != nil {
		return json.Marshal(s.Group)
	}
	return nil, fmt.Errorf("StepOrGroup is empty")
}

// Item represents a single item in the workflow sequence: either a step or a group.
type Item struct {
	Type  string
	Step  Step
	Group ParallelGroup
}

// Items returns the workflow sequence as a list of items.
func (wf *Workflow) Items() []Item {
	var items []Item
	for _, sog := range wf.Steps {
		if sog.Step != nil {
			items = append(items, Item{Type: "step", Step: *sog.Step})
		} else if sog.Group != nil {
			items = append(items, Item{Type: "group", Group: *sog.Group})
		}
	}
	return items
}

// FlatStep is a step flattened from the nested workflow structure with group metadata.
type FlatStep struct {
	Step           Step
	GroupID        string
	GroupName      string
	IsFirstInGroup bool
	IsLastInGroup  bool
	ItemIndex      int
}

// FlatSteps returns all steps in the workflow as a flattened list.
// Steps inside groups are annotated with their group metadata.
func (wf *Workflow) FlatSteps() []FlatStep {
	var flat []FlatStep
	items := wf.Items()
	for itemIdx, item := range items {
		if item.Type == "step" {
			flat = append(flat, FlatStep{
				Step:      item.Step,
				ItemIndex: itemIdx,
			})
		} else {
			group := item.Group
			for i, step := range group.Steps {
				flat = append(flat, FlatStep{
					Step:           step,
					GroupID:        group.ID,
					GroupName:      group.Name,
					IsFirstInGroup: i == 0,
					IsLastInGroup:  i == len(group.Steps)-1,
					ItemIndex:      itemIdx,
				})
			}
		}
	}
	return flat
}

// Workflow is the top-level structure loaded from JSON.
type Workflow struct {
	Name        string               `json:"name"`
	Description string               `json:"description,omitempty"`
	Parameters  map[string]Parameter `json:"parameters,omitempty"`
	Steps       []StepOrGroup        `json:"steps"`
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
	for i, sog := range wf.Steps {
		if sog.Step != nil {
			step := sog.Step
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
		} else if sog.Group != nil {
			group := sog.Group
			if group.ID == "" {
				return fmt.Errorf("group %d: id is required", i)
			}
			if group.Name == "" {
				return fmt.Errorf("group %d: name is required", i)
			}
			if len(group.Steps) == 0 {
				return fmt.Errorf("group %d: must have at least one step", i)
			}
			if _, ok := seenIDs[group.ID]; ok {
				return fmt.Errorf("group %d: duplicate id %q", i, group.ID)
			}
			seenIDs[group.ID] = struct{}{}
			for j, step := range group.Steps {
				if step.ID == "" {
					return fmt.Errorf("group %d, step %d: id is required", i, j)
				}
				if step.Name == "" {
					return fmt.Errorf("group %d, step %d: name is required", i, j)
				}
				if step.Script == "" {
					return fmt.Errorf("group %d, step %d: script is required", i, j)
				}
				if _, ok := seenIDs[step.ID]; ok {
					return fmt.Errorf("group %d, step %d: duplicate id %q", i, j, step.ID)
				}
				seenIDs[step.ID] = struct{}{}

				if step.OutputType != "" && step.OutputType != OutputText && step.OutputType != OutputMarkdown {
					return fmt.Errorf("group %d, step %d: unknown output_type %q", i, j, step.OutputType)
				}

				seenParams := make(map[string]struct{})
				for _, param := range step.Params {
					if _, ok := seenParams[param]; ok {
						return fmt.Errorf("group %d, step %q: duplicate parameter %q", i, step.ID, param)
					}
					seenParams[param] = struct{}{}
					if _, ok := wf.Parameters[param]; !ok {
						return fmt.Errorf("group %d, step %q references undefined parameter %q", i, step.ID, param)
					}
				}
			}
		} else {
			return fmt.Errorf("step %d: invalid step or group", i)
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
