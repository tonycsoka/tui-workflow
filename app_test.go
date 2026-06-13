package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadWorkflow(t *testing.T) {
	wf, err := LoadWorkflow("examples/deploy.json")
	if err != nil {
		t.Fatalf("Failed to load workflow: %v", err)
	}
	if wf.Name != "deploy" {
		t.Errorf("Expected workflow name 'deploy', got %q", wf.Name)
	}
	if len(wf.Steps) != 2 {
		t.Errorf("Expected 2 steps, got %d", len(wf.Steps))
	}
	if len(wf.Parameters) != 2 {
		t.Errorf("Expected 2 parameters, got %d", len(wf.Parameters))
	}
}

func TestSessionLoadSave(t *testing.T) {
	wf, err := LoadWorkflow("examples/deploy.json")
	if err != nil {
		t.Fatalf("Failed to load workflow: %v", err)
	}

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get working directory: %v", err)
	}
	sess := NewSession(wf, cwd)
	sess.SetParameterValue("env", "staging")
	sess.SetParameterValue("version", "2.0.0")

	if err := SaveSession(sess); err != nil {
		t.Fatalf("Failed to save session: %v", err)
	}

	loaded, err := LoadSessionByName(wf.Name, cwd, sess.Name)
	if err != nil {
		t.Fatalf("Failed to load session: %v", err)
	}
	if loaded == nil {
		t.Fatal("Expected session to be loaded, got nil")
	}
	if loaded.ParameterValues["env"] != "staging" {
		t.Errorf("Expected env=staging, got %q", loaded.ParameterValues["env"])
	}
	if loaded.ParameterValues["version"] != "2.0.0" {
		t.Errorf("Expected version=2.0.0, got %q", loaded.ParameterValues["version"])
	}

	os.RemoveAll(filepath.Join(SessionDir(), cwdHash(cwd), wf.Name))
}

func TestResolveScriptPath(t *testing.T) {
	abs := ResolveScriptPath("/home/user", "/usr/bin/script.sh")
	if abs != "/usr/bin/script.sh" {
		t.Errorf("Expected absolute path preserved, got %q", abs)
	}

	rel := ResolveScriptPath("/home/user", "scripts/build.sh")
	expected := filepath.Join("/home/user", "scripts/build.sh")
	if rel != expected {
		t.Errorf("Expected %q, got %q", expected, rel)
	}
}

func TestStepSequencing(t *testing.T) {
	wf, err := LoadWorkflow("examples/deploy.json")
	if err != nil {
		t.Fatalf("Failed to load workflow: %v", err)
	}
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get working directory: %v", err)
	}
	sess := NewSession(wf, cwd)

	if !sess.IsStepRunnable(wf, 0) {
		t.Error("Step 0 should be runnable")
	}
	if sess.IsStepRunnable(wf, 1) {
		t.Error("Step 1 should not be runnable yet")
	}

	sess.UpdateStepState(wf.Steps[0].ID, StepState{Status: StatusSuccess})

	if !sess.IsStepRunnable(wf, 1) {
		t.Error("Step 1 should be runnable after step 0 succeeds")
	}

	sess.UpdateStepState(wf.Steps[1].ID, StepState{Status: StatusSkipped})
}

func TestRunOncePerSession(t *testing.T) {
	wf, err := LoadWorkflow("examples/deploy.json")
	if err != nil {
		t.Fatalf("Failed to load workflow: %v", err)
	}
	wf.Steps[0].RunOncePerSession = true
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get working directory: %v", err)
	}
	sess := NewSession(wf, cwd)

	if !sess.IsStepRunnable(wf, 0) {
		t.Error("Step 0 should be runnable initially")
	}

	sess.UpdateStepState(wf.Steps[0].ID, StepState{Status: StatusSuccess})

	if sess.IsStepRunnable(wf, 0) {
		t.Error("Step 0 should not be runnable again after success with run_once_per_session")
	}
}

func TestRunOncePerSessionSkipped(t *testing.T) {
	wf, err := LoadWorkflow("examples/deploy.json")
	if err != nil {
		t.Fatalf("Failed to load workflow: %v", err)
	}
	wf.Steps[0].RunOncePerSession = true
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get working directory: %v", err)
	}
	sess := NewSession(wf, cwd)

	if !sess.IsStepRunnable(wf, 0) {
		t.Error("Step 0 should be runnable initially")
	}

	sess.UpdateStepState(wf.Steps[0].ID, StepState{Status: StatusSkipped})

	if sess.IsStepRunnable(wf, 0) {
		t.Error("Step 0 should not be runnable after being skipped with run_once_per_session")
	}
}

func TestViewRendersSteps(t *testing.T) {
	wf, err := LoadWorkflow("examples/deploy.json")
	if err != nil {
		t.Fatalf("Failed to load workflow: %v", err)
	}
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get working directory: %v", err)
	}
	sess := NewSession(wf, cwd)
	m := initialModel(wf, sess, "examples")
	m.width = 100
	m.height = 30
	m.resizeViewports()

	view := m.View()
	if !strings.Contains(view.Content, "Build") {
		t.Errorf("View should contain 'Build', got:\n%s", view.Content)
	}
	if !strings.Contains(view.Content, "Deploy") {
		t.Errorf("View should contain 'Deploy', got:\n%s", view.Content)
	}
	if !strings.Contains(view.Content, "env") {
		t.Errorf("View should contain 'env', got:\n%s", view.Content)
	}
	if !strings.Contains(view.Content, "version") {
		t.Errorf("View should contain 'version', got:\n%s", view.Content)
	}
	if !strings.Contains(view.Content, "Steps") {
		t.Errorf("View should contain 'Steps' header, got:\n%s", view.Content)
	}
	if !strings.Contains(view.Content, "pending") {
		t.Errorf("View should contain 'pending' status, got:\n%s", view.Content)
	}
}

func TestViewRendersStepsSmallTerminal(t *testing.T) {
	wf, err := LoadWorkflow("examples/deploy.json")
	if err != nil {
		t.Fatalf("Failed to load workflow: %v", err)
	}
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get working directory: %v", err)
	}
	sess := NewSession(wf, cwd)
	m := initialModel(wf, sess, "examples")
	m.width = 50
	m.height = 16 // minimum for 2 params with bodyH = height - 1
	m.resizeViewports()

	view := m.View()
	if !strings.Contains(view.Content, "Build") {
		t.Errorf("View should contain 'Build' in small terminal, got:\n%s", view.Content)
	}
	if !strings.Contains(view.Content, "Deploy") {
		t.Errorf("View should contain 'Deploy' in small terminal, got:\n%s", view.Content)
	}
}

func TestViewSmallTerminal(t *testing.T) {
	wf, err := LoadWorkflow("examples/deploy.json")
	if err != nil {
		t.Fatalf("Failed to load workflow: %v", err)
	}
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get working directory: %v", err)
	}
	sess := NewSession(wf, cwd)
	m := initialModel(wf, sess, "examples")
	m.width = 40
	m.height = 10
	m.resizeViewports()

	view := m.View()
	if !strings.Contains(view.Content, "Build") {
		t.Errorf("Expected 'Build' in small terminal view, got:\n%s", view.Content)
	}
}

func TestViewDebug(t *testing.T) {
	wf, err := LoadWorkflow("examples/deploy.json")
	if err != nil {
		t.Fatalf("Failed to load workflow: %v", err)
	}
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get working directory: %v", err)
	}
	sess := NewSession(wf, cwd)
	m := initialModel(wf, sess, "examples")
	m.width = 100
	m.height = 30
	m.resizeViewports()

	view := m.View()
	if len(view.Content) == 0 {
		t.Fatal("View is empty")
	}
	if !strings.Contains(view.Content, "Build") || !strings.Contains(view.Content, "Deploy") {
		t.Fatalf("View missing steps:\n%s", view.Content)
	}
	if !strings.Contains(view.Content, "Parameters") {
		t.Fatalf("View missing 'Parameters' label:\n%s", view.Content)
	}
	if !strings.Contains(view.Content, "Stdout") {
		t.Fatalf("View missing 'Stdout' label:\n%s", view.Content)
	}
	if !strings.Contains(view.Content, "Stderr") {
		t.Fatalf("View missing 'Stderr' label:\n%s", view.Content)
	}
}

func TestMarkdownRendering(t *testing.T) {
	wf, err := LoadWorkflow("examples/deploy.json")
	if err != nil {
		t.Fatalf("Failed to load workflow: %v", err)
	}
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get working directory: %v", err)
	}
	sess := NewSession(wf, cwd)
	m := initialModel(wf, sess, "examples")
	m.width = 100
	m.height = 30
	m.resizeViewports()

	rendered, err := m.renderMarkdown("# Hello\n\nWorld", 80)
	if err != nil {
		t.Fatalf("renderMarkdown failed: %v", err)
	}
	if rendered == "" {
		t.Fatal("renderMarkdown returned empty string")
	}
	if !strings.Contains(rendered, "Hello") {
		t.Errorf("rendered markdown should contain 'Hello', got:\n%s", rendered)
	}
}

func TestWorkflowValidationUnknownOutputType(t *testing.T) {
	wf := Workflow{
		Name: "test",
		Steps: []Step{
			{ID: "s1", Name: "Step 1", Script: "foo.sh", OutputType: "bad"},
		},
	}
	if err := wf.Validate(); err == nil {
		t.Error("Expected validation error for unknown output_type")
	}
}

func TestWorkflowValidationDuplicateParam(t *testing.T) {
	wf := Workflow{
		Name: "test",
		Parameters: map[string]Parameter{
			"env": {Type: ParamString},
		},
		Steps: []Step{
			{ID: "s1", Name: "Step 1", Script: "foo.sh", Params: []string{"env", "env"}},
		},
	}
	if err := wf.Validate(); err == nil {
		t.Error("Expected validation error for duplicate parameter")
	}
}
