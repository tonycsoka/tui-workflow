package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
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

func TestLoadWorkflowWithAutoRun(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "auto_run.json")
	content := `{
		"name": "auto-run-test",
		"steps": [
			{"id": "s1", "name": "Step 1", "script": "a.sh", "auto_run": true},
			{"id": "s2", "name": "Step 2", "script": "b.sh", "auto_run": false},
			{"id": "s3", "name": "Step 3", "script": "c.sh"}
		]
	}`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	wf, err := LoadWorkflow(path)
	if err != nil {
		t.Fatalf("Failed to load workflow: %v", err)
	}
	if len(wf.Steps) != 3 {
		t.Fatalf("Expected 3 steps, got %d", len(wf.Steps))
	}
	if !wf.Steps[0].AutoRun {
		t.Error("Expected step 0 auto_run=true")
	}
	if wf.Steps[1].AutoRun {
		t.Error("Expected step 1 auto_run=false")
	}
	if wf.Steps[2].AutoRun {
		t.Error("Expected step 2 auto_run=false (default)")
	}
}

func TestAutoRunChain(t *testing.T) {
	if os.Getenv("SKIP_SHELL_TESTS") != "" {
		t.Skip("skipping shell-based auto-run chain test")
	}

	cwd := t.TempDir()
	script1 := filepath.Join(cwd, "step1.sh")
	script2 := filepath.Join(cwd, "step2.sh")
	if err := os.WriteFile(script1, []byte("#!/bin/sh\necho step1\n"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(script2, []byte("#!/bin/sh\necho step2\n"), 0755); err != nil {
		t.Fatal(err)
	}

	wf := Workflow{
		Name: "test",
		Steps: []Step{
			{ID: "s1", Name: "Step 1", Script: "step1.sh", AutoRun: false},
			{ID: "s2", Name: "Step 2", Script: "step2.sh", AutoRun: true},
		},
	}
	sess := NewSession(&wf, cwd)
	m := initialModel(&wf, sess, cwd)
	m.width = 100
	m.height = 30
	m.resizeViewports()

	// Simulate step 0 finishing while auto-run is active
	m.autoRun = true
	m.currentStepID = "s1"
	m.runner = nil
	m.liveOutputs["s1"] = &liveOutput{stdout: []byte("step1 output\n")}

	newModel, _ := m.Update(shellDoneMsg{stepID: "s1", status: StatusSuccess, exitCode: 0})
	newM := newModel.(model)

	if newM.cursor != 1 {
		t.Errorf("Expected cursor=1 after chaining, got %d", newM.cursor)
	}
	if !newM.autoRun {
		t.Error("Expected autoRun=true after chaining to step 2")
	}
	if newM.currentStepID != "s2" {
		t.Errorf("Expected currentStepID=s2, got %s", newM.currentStepID)
	}
	if sess.StepStates["s2"].Status != StatusRunning {
		t.Errorf("Expected step 2 status=running, got %v", sess.StepStates["s2"].Status)
	}
}

func TestAutoRunChainStops(t *testing.T) {
	if os.Getenv("SKIP_SHELL_TESTS") != "" {
		t.Skip("skipping shell-based auto-run chain test")
	}

	cwd := t.TempDir()
	script1 := filepath.Join(cwd, "step1.sh")
	script2 := filepath.Join(cwd, "step2.sh")
	if err := os.WriteFile(script1, []byte("#!/bin/sh\necho step1\n"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(script2, []byte("#!/bin/sh\necho step2\n"), 0755); err != nil {
		t.Fatal(err)
	}

	wf := Workflow{
		Name: "test",
		Steps: []Step{
			{ID: "s1", Name: "Step 1", Script: "step1.sh", AutoRun: false},
			{ID: "s2", Name: "Step 2", Script: "step2.sh", AutoRun: false},
		},
	}
	sess := NewSession(&wf, cwd)
	m := initialModel(&wf, sess, cwd)
	m.width = 100
	m.height = 30
	m.resizeViewports()

	m.autoRun = true
	m.currentStepID = "s1"
	m.runner = nil
	m.liveOutputs["s1"] = &liveOutput{stdout: []byte("step1 output\n")}

	newModel, _ := m.Update(shellDoneMsg{stepID: "s1", status: StatusSuccess, exitCode: 0})
	newM := newModel.(model)

	if newM.cursor != 1 {
		t.Errorf("Expected cursor=1 after advance, got %d", newM.cursor)
	}
	if newM.autoRun {
		t.Error("Expected autoRun=false when next step is not auto_run")
	}
	if newM.currentStepID != "" {
		t.Errorf("Expected currentStepID=\"\", got %s", newM.currentStepID)
	}
}

func TestAutoRunChainStopsOnFailure(t *testing.T) {
	cwd := t.TempDir()
	script1 := filepath.Join(cwd, "step1.sh")
	script2 := filepath.Join(cwd, "step2.sh")
	if err := os.WriteFile(script1, []byte("#!/bin/sh\nexit 1\n"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(script2, []byte("#!/bin/sh\necho step2\n"), 0755); err != nil {
		t.Fatal(err)
	}

	wf := Workflow{
		Name: "test",
		Steps: []Step{
			{ID: "s1", Name: "Step 1", Script: "step1.sh", AutoRun: false},
			{ID: "s2", Name: "Step 2", Script: "step2.sh", AutoRun: true},
		},
	}
	sess := NewSession(&wf, cwd)
	m := initialModel(&wf, sess, cwd)
	m.width = 100
	m.height = 30
	m.resizeViewports()

	m.autoRun = true
	m.currentStepID = "s1"
	m.runner = nil
	m.liveOutputs["s1"] = &liveOutput{stdout: []byte("step1 output\n")}

	newModel, _ := m.Update(shellDoneMsg{stepID: "s1", status: StatusFailed, exitCode: 1})
	newM := newModel.(model)

	if newM.cursor != 0 {
		t.Errorf("Expected cursor=0 after failed step, got %d", newM.cursor)
	}
	if newM.autoRun {
		t.Error("Expected autoRun=false after step failure")
	}
	if newM.currentStepID != "" {
		t.Errorf("Expected currentStepID=\"\", got %s", newM.currentStepID)
	}
}

func TestTabSwitching(t *testing.T) {
	wf := Workflow{
		Name: "test",
		Steps: []Step{
			{ID: "s1", Name: "Step 1", Script: "foo.sh"},
		},
	}
	sess := NewSession(&wf, ".")
	m := initialModel(&wf, sess, ".")
	m.width = 100
	m.height = 30
	m.resizeViewports()

	// Right arrow switches to stderr
	newM, _ := m.handleKeyMsg(tea.KeyPressMsg{Code: tea.KeyRight})
	if newM.outputTab != 1 {
		t.Errorf("Expected outputTab=1 after right arrow, got %d", newM.outputTab)
	}

	// Right arrow wraps to stdout
	newM, _ = newM.handleKeyMsg(tea.KeyPressMsg{Code: tea.KeyRight})
	if newM.outputTab != 0 {
		t.Errorf("Expected outputTab=0 after wrapping right arrow, got %d", newM.outputTab)
	}

	// Left arrow switches to stderr
	newM, _ = newM.handleKeyMsg(tea.KeyPressMsg{Code: tea.KeyLeft})
	if newM.outputTab != 1 {
		t.Errorf("Expected outputTab=1 after left arrow, got %d", newM.outputTab)
	}

	// Left arrow wraps to stdout
	newM, _ = newM.handleKeyMsg(tea.KeyPressMsg{Code: tea.KeyLeft})
	if newM.outputTab != 0 {
		t.Errorf("Expected outputTab=0 after wrapping left arrow, got %d", newM.outputTab)
	}
}

func TestAllParamsSet(t *testing.T) {
	wf := Workflow{
		Name: "test",
		Parameters: map[string]Parameter{
			"required": {Type: ParamString, Description: "Required param"},
			"optional": {Type: ParamString, Default: "default"},
		},
		Steps: []Step{
			{ID: "s1", Name: "Step 1", Script: "foo.sh"},
		},
	}
	sess := NewSession(&wf, ".")
	m := initialModel(&wf, sess, ".")

	if m.allParamsSet() {
		t.Error("allParamsSet should be false when required parameter is not set")
	}

	// Set required parameter via session
	sess.SetParameterValue("required", "value")
	m.updateParamInputs()

	if !m.allParamsSet() {
		t.Error("allParamsSet should be true after setting required parameter")
	}

	// Optional parameter with default is already set
	// If we explicitly set it to empty, it should still be considered set
	// because GetParameterValue returns the default.
	// If we clear the default by setting it to empty string, the value is empty
	// but GetParameterValue will return the default. So allParamsSet is about
	// the final resolved value, not whether the user typed something.
	sess.SetParameterValue("required", "")
	m.updateParamInputs()

	if m.allParamsSet() {
		t.Error("allParamsSet should be false when required parameter is empty and has no default")
	}
}

func TestCanRunBlockedByMissingParams(t *testing.T) {
	wf := Workflow{
		Name: "test",
		Parameters: map[string]Parameter{
			"required": {Type: ParamString, Description: "Required param"},
		},
		Steps: []Step{
			{ID: "s1", Name: "Step 1", Script: "foo.sh"},
		},
	}
	sess := NewSession(&wf, ".")
	m := initialModel(&wf, sess, ".")
	m.width = 100
	m.height = 30
	m.resizeViewports()

	if m.canRun() {
		t.Error("canRun should be false when required parameter is missing")
	}

	// Set required parameter
	sess.SetParameterValue("required", "value")
	m.updateParamInputs()

	if !m.canRun() {
		t.Error("canRun should be true after setting required parameter")
	}
}

func TestFooterWarnsMissingParams(t *testing.T) {
	wf := Workflow{
		Name: "test",
		Parameters: map[string]Parameter{
			"required": {Type: ParamString, Description: "Required param"},
		},
		Steps: []Step{
			{ID: "s1", Name: "Step 1", Script: "foo.sh"},
		},
	}
	sess := NewSession(&wf, ".")
	m := initialModel(&wf, sess, ".")
	m.width = 100
	m.height = 30
	m.resizeViewports()

	view := m.View()
	if !strings.Contains(view.Content, "set all parameters to run") {
		t.Errorf("Footer should warn about missing parameters, got:\n%s", view.Content)
	}

	// Set required parameter
	sess.SetParameterValue("required", "value")
	m.updateParamInputs()
	view = m.View()
	if strings.Contains(view.Content, "set all parameters to run") {
		t.Errorf("Footer should not warn after setting parameters, got:\n%s", view.Content)
	}
}

func TestInitialModelLoadsStepOutput(t *testing.T) {
	wf := Workflow{
		Name: "test",
		Steps: []Step{
			{ID: "s1", Name: "Step 1", Script: "foo.sh"},
		},
	}
	sess := NewSession(&wf, ".")
	sess.UpdateStepState("s1", StepState{
		Status: StatusSuccess,
		Stdout: "hello stdout",
		Stderr: "hello stderr",
	})

	m := initialModel(&wf, sess, ".")
	if string(m.stdoutBuffer) != "hello stdout" {
		t.Errorf("Expected stdoutBuffer to be loaded on init, got %q", string(m.stdoutBuffer))
	}
	if string(m.stderrBuffer) != "hello stderr" {
		t.Errorf("Expected stderrBuffer to be loaded on init, got %q", string(m.stderrBuffer))
	}
}

func TestAutoRunIcon(t *testing.T) {
	wf := Workflow{
		Name: "test",
		Steps: []Step{
			{ID: "s1", Name: "Step 1", Script: "foo.sh", AutoRun: true},
			{ID: "s2", Name: "Step 2", Script: "bar.sh", AutoRun: false},
		},
	}
	cwd := t.TempDir()
	sess := NewSession(&wf, cwd)
	m := initialModel(&wf, sess, cwd)
	m.width = 100
	m.height = 30
	m.resizeViewports()

	view := m.View()
	if !strings.Contains(view.Content, "⏵") {
		t.Errorf("View should contain auto-run icon ⏵, got:\n%s", view.Content)
	}
	// Verify that the non-auto-run step still shows the repeatable icon
	if !strings.Contains(view.Content, "↻") {
		t.Errorf("View should contain repeatable icon ↻, got:\n%s", view.Content)
	}
}
