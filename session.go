package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

func logWarning(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, "tui-workflow: "+format+"\n", args...)
}

// StepStatus represents the execution state of a step.
type StepStatus string

const (
	StatusPending   StepStatus = "pending"
	StatusRunning   StepStatus = "running"
	StatusSuccess   StepStatus = "success"
	StatusFailed    StepStatus = "failed"
	StatusSkipped   StepStatus = "skipped"
)

// StepState tracks the execution state and output of a single step.
type StepState struct {
	Status   StepStatus `json:"status"`
	ExitCode int        `json:"exit_code,omitempty"`
	RunAt    string     `json:"run_at,omitempty"`
	// Output is deprecated. Use Stdout and Stderr instead.
	// Kept for backward compatibility with sessions created before the split.
	//
	// Deprecated: Use Stdout and Stderr instead.
	Output   string     `json:"output,omitempty"`
	Stdout   string     `json:"stdout,omitempty"`
	Stderr   string     `json:"stderr,omitempty"`
}

// GetOutput returns the stdout and stderr for a step.
// For backward compatibility, it falls back to parsing the old combined Output field.
func (s StepState) GetOutput() (stdout, stderr string) {
	if s.Stdout != "" || s.Stderr != "" {
		return s.Stdout, s.Stderr
	}
	if s.Output == "" {
		return "", ""
	}
	// Backward compat: parse old combined format
	parts := strings.SplitN(s.Output, "\n--- stderr ---\n", 2)
	if len(parts) == 2 {
		return parts[0], parts[1]
	}
	return s.Output, ""
}

// Session is the persisted state for a workflow run in a specific directory.
type Session struct {
	Name            string                 `json:"name"`
	WorkflowName    string                 `json:"workflow_name"`
	Cwd             string                 `json:"cwd"`
	CreatedAt       string                 `json:"created_at"`
	ParameterValues map[string]string      `json:"parameter_values"`
	StepStates      map[string]StepState   `json:"step_states"`
}

// NewSession creates a fresh session for the given workflow and directory.
// The session name is auto-generated from the current datetime.
func NewSession(wf *Workflow, cwd string) *Session {
	stepStates := make(map[string]StepState)
	for _, step := range wf.Steps {
		stepStates[step.ID] = StepState{Status: StatusPending}
	}

	now := time.Now()
	return &Session{
		Name:            now.Format("2006-01-02T15:04:05.000"),
		WorkflowName:    wf.Name,
		Cwd:             cwd,
		CreatedAt:       now.Format(time.RFC3339),
		ParameterValues: make(map[string]string),
		StepStates:      stepStates,
	}
}

// SessionDir returns the directory where session files are stored.
func SessionDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ".tui-workflow/sessions"
	}
	return filepath.Join(home, ".local", "share", "tui-workflow", "sessions")
}

// cwdHash returns the first 8 bytes of the SHA256 hash of the cwd.
func cwdHash(cwd string) string {
	hash := sha256.Sum256([]byte(cwd))
	return hex.EncodeToString(hash[:8])
}

// SessionPath returns the file path for a session based on workflow name, cwd, and session name.
// Structure: ~/.local/share/tui-workflow/sessions/<cwd-hash>/<workflow-name>/<session-name>.json
func SessionPath(workflowName, cwd, sessionName string) string {
	// Sanitize colons to dashes so the filename is safe on all filesystems.
	safeName := strings.ReplaceAll(sessionName, ":", "-")
	return filepath.Join(SessionDir(), cwdHash(cwd), workflowName, safeName+".json")
}

func parseSession(data []byte) (*Session, error) {
	var sess Session
	if err := json.Unmarshal(data, &sess); err != nil {
		return nil, fmt.Errorf("parsing session JSON: %w", err)
	}
	if sess.StepStates == nil {
		sess.StepStates = make(map[string]StepState)
	}
	if sess.ParameterValues == nil {
		sess.ParameterValues = make(map[string]string)
	}
	return &sess, nil
}

// LoadSessionByName reads a specific named session from disk.
func LoadSessionByName(workflowName, cwd, sessionName string) (*Session, error) {
	path := SessionPath(workflowName, cwd, sessionName)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading session file: %w", err)
	}
	return parseSession(data)
}

// LoadSessionFromPath reads a session from a given file path.
func LoadSessionFromPath(path string) (*Session, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading session file: %w", err)
	}
	return parseSession(data)
}

// SaveSession writes the session to disk, creating directories if needed.
func SaveSession(sess *Session) error {
	path := SessionPath(sess.WorkflowName, sess.Cwd, sess.Name)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("creating session directory: %w", err)
	}

	data, err := json.MarshalIndent(sess, "", "  ")
	if err != nil {
		return fmt.Errorf("marshalling session: %w", err)
	}

	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("writing session file: %w", err)
	}
	return nil
}

// FindSessionsForWorkflow returns all sessions for a given workflow and directory.
func FindSessionsForWorkflow(workflowName, cwd string) ([]*Session, error) {
	dir := filepath.Join(SessionDir(), cwdHash(cwd), workflowName)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var sessions []*Session
	for _, entry := range entries {
		name := entry.Name()
		if !entry.IsDir() && filepath.Ext(name) == ".json" {
			path := filepath.Join(dir, name)
			sess, err := LoadSessionFromPath(path)
			if err != nil {
				logWarning("skipping corrupted session file %s: %v", path, err)
				continue
			}
			if sess != nil {
				sessions = append(sessions, sess)
			}
		}
	}

	// Sort by CreatedAt descending (most recent first)
	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].CreatedAt > sessions[j].CreatedAt
	})

	return sessions, nil
}

// OverallStatus returns the overall status of the session.
func (sess *Session) OverallStatus() string {
	total := len(sess.StepStates)
	if total == 0 {
		return "empty"
	}

	done := 0
	failed := 0
	running := 0
	pending := 0
	for _, state := range sess.StepStates {
		switch state.Status {
		case StatusSuccess, StatusSkipped:
			done++
		case StatusFailed:
			failed++
		case StatusRunning:
			running++
		case StatusPending:
			pending++
		}
	}

	if done == total {
		return "done"
	}
	if failed > 0 {
		return "failed"
	}
	if running > 0 {
		return "running"
	}
	if pending == total {
		return "pending"
	}
	return "in progress"
}

// UpdateStepState updates a step's state and sets its run timestamp.
func (sess *Session) UpdateStepState(stepID string, state StepState) {
	if sess.StepStates == nil {
		sess.StepStates = make(map[string]StepState)
	}
	state.RunAt = time.Now().Format(time.RFC3339)
	sess.StepStates[stepID] = state
}

// SetParameterValue sets a parameter value.
func (sess *Session) SetParameterValue(key, value string) {
	if sess.ParameterValues == nil {
		sess.ParameterValues = make(map[string]string)
	}
	sess.ParameterValues[key] = value
}

// GetParameterValue returns the parameter value, or the default from the workflow.
func (sess *Session) GetParameterValue(key string, wf *Workflow) string {
	if val, ok := sess.ParameterValues[key]; ok {
		return val
	}
	if def, ok := wf.Parameters[key]; ok {
		return def.Default
	}
	return ""
}

// IsStepRunnable checks whether a step is eligible to run based on sequence and run_once.
func (sess *Session) IsStepRunnable(wf *Workflow, idx int) bool {
	if idx < 0 || idx >= len(wf.Steps) {
		return false
	}
	step := wf.Steps[idx]
	state := sess.StepStates[step.ID]

	// If it's already running, don't run again.
	if state.Status == StatusRunning {
		return false
	}

	// If run_once_per_session and already succeeded, skip.
	if step.RunOncePerSession && state.Status == StatusSuccess {
		return false
	}

	// If run_once_per_session and already skipped, skip.
	if step.RunOncePerSession && state.Status == StatusSkipped {
		return false
	}

	// First step is always runnable if not already running/success.
	if idx == 0 {
		return state.Status == StatusPending || state.Status == StatusFailed || state.Status == StatusSkipped
	}

	// Previous step must be success or skipped.
	prevStep := wf.Steps[idx-1]
	prevState := sess.StepStates[prevStep.ID]
	return prevState.Status == StatusSuccess || prevState.Status == StatusSkipped
}

