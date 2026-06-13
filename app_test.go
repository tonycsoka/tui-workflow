
package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestParallelGroupJSONLoading(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "parallel.json")
	content := `{
		"name": "parallel-test",
		"steps": [
			{"id": "build", "name": "Build", "script": "scripts/build.sh"},
			{
				"id": "tests",
				"name": "Test Suite",
				"steps": [
					{"id": "unit", "name": "Unit Tests", "script": "scripts/unit.sh"},
					{"id": "lint", "name": "Lint", "script": "scripts/lint.sh"}
				]
			},
			{"id": "deploy", "name": "Deploy", "script": "scripts/deploy.sh"}
		]
	}`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	wf, err := LoadWorkflow(path)
	if err != nil {
		t.Fatalf("Failed to load parallel workflow: %v", err)
	}
	if len(wf.Steps) != 3 {
		t.Fatalf("Expected 3 items, got %d", len(wf.Steps))
	}
	if wf.Steps[0].Step == nil || wf.Steps[0].Step.ID != "build" {
		t.Error("Expected first item to be a step with id=build")
	}
	if wf.Steps[1].Group == nil || wf.Steps[1].Group.ID != "tests" {
		t.Error("Expected second item to be a group with id=tests")
	}
	if len(wf.Steps[1].Group.Steps) != 2 {
		t.Errorf("Expected group to have 2 steps, got %d", len(wf.Steps[1].Group.Steps))
	}
	if wf.Steps[2].Step == nil || wf.Steps[2].Step.ID != "deploy" {
		t.Error("Expected third item to be a step with id=deploy")
	}

	// Check FlatSteps
	flat := wf.FlatSteps()
	if len(flat) != 4 {
		t.Fatalf("Expected 4 flat steps, got %d", len(flat))
	}
	if flat[0].Step.ID != "build" || flat[0].GroupID != "" {
		t.Error("Expected flat step 0 to be build with no group")
	}
	if flat[1].Step.ID != "unit" || flat[1].GroupID != "tests" {
		t.Error("Expected flat step 1 to be unit in group tests")
	}
	if flat[2].Step.ID != "lint" || flat[2].GroupID != "tests" {
		t.Error("Expected flat step 2 to be lint in group tests")
	}
	if flat[3].Step.ID != "deploy" || flat[3].GroupID != "" {
		t.Error("Expected flat step 3 to be deploy with no group")
	}
}

func TestParallelGroupSequencing(t *testing.T) {
	wf := Workflow{
		Name: "test",
		Steps: []StepOrGroup{
			{Step: &Step{ID: "s1", Name: "Step 1", Script: "foo.sh"}},
			{
				Group: &ParallelGroup{
					ID:   "g1",
					Name: "Group 1",
					Steps: []Step{
						{ID: "g1a", Name: "G1A", Script: "a.sh"},
						{ID: "g1b", Name: "G1B", Script: "b.sh"},
					},
				},
			},
			{Step: &Step{ID: "s2", Name: "Step 2", Script: "bar.sh"}},
		},
	}
	sess := NewSession(&wf, ".")

	// Step 0 should be runnable
	if !sess.IsStepRunnable(&wf, 0) {
		t.Error("Step 0 should be runnable")
	}
	// Group steps should not be runnable yet
	if sess.IsStepRunnable(&wf, 1) {
		t.Error("Group step 1 should not be runnable yet")
	}
	if sess.IsStepRunnable(&wf, 2) {
		t.Error("Group step 2 should not be runnable yet")
	}
	// Step after group should not be runnable yet
	if sess.IsStepRunnable(&wf, 3) {
		t.Error("Step 3 should not be runnable yet")
	}

	// Complete step 0
	sess.UpdateStepState("s1", StepState{Status: StatusSuccess})

	// Now group steps should be runnable
	if !sess.IsStepRunnable(&wf, 1) {
		t.Error("Group step 1 should be runnable after step 0 succeeds")
	}
	if !sess.IsStepRunnable(&wf, 2) {
		t.Error("Group step 2 should be runnable after step 0 succeeds")
	}
	// Step after group should still not be runnable
	if sess.IsStepRunnable(&wf, 3) {
		t.Error("Step 3 should not be runnable until group completes")
	}

	// Complete one group step
	sess.UpdateStepState("g1a", StepState{Status: StatusSuccess})
	// Step 3 should still not be runnable
	if sess.IsStepRunnable(&wf, 3) {
		t.Error("Step 3 should not be runnable until all group steps complete")
	}

	// Complete the other group step
	sess.UpdateStepState("g1b", StepState{Status: StatusSuccess})
	// Now step 3 should be runnable
	if !sess.IsStepRunnable(&wf, 3) {
		t.Error("Step 3 should be runnable after group completes")
	}
}

func TestParallelGroupFailureBlocksDownstream(t *testing.T) {
	wf := Workflow{
		Name: "test",
		Steps: []StepOrGroup{
			{
				Group: &ParallelGroup{
					ID:   "g1",
					Name: "Group 1",
					Steps: []Step{
						{ID: "g1a", Name: "G1A", Script: "a.sh"},
						{ID: "g1b", Name: "G1B", Script: "b.sh"},
					},
				},
			},
			{Step: &Step{ID: "s2", Name: "Step 2", Script: "bar.sh"}},
		},
	}
	sess := NewSession(&wf, ".")

	// Complete one group step, fail the other
	sess.UpdateStepState("g1a", StepState{Status: StatusSuccess})
	sess.UpdateStepState("g1b", StepState{Status: StatusFailed})

	// Group status should be failed
	items := wf.Items()
	if sess.ItemStatus(items[0]) != StatusFailed {
		t.Errorf("Expected group status failed, got %v", sess.ItemStatus(items[0]))
	}

	// Downstream should be blocked
	if sess.IsStepRunnable(&wf, 2) {
		t.Error("Step 2 should be blocked after group failure")
	}
}

func TestParallelGroupSkippedAllowsDownstream(t *testing.T) {
	wf := Workflow{
		Name: "test",
		Steps: []StepOrGroup{
			{
				Group: &ParallelGroup{
					ID:   "g1",
					Name: "Group 1",
					Steps: []Step{
						{ID: "g1a", Name: "G1A", Script: "a.sh"},
						{ID: "g1b", Name: "G1B", Script: "b.sh"},
					},
				},
			},
			{Step: &Step{ID: "s2", Name: "Step 2", Script: "bar.sh"}},
		},
	}
	sess := NewSession(&wf, ".")

	// Skip both group steps
	sess.UpdateStepState("g1a", StepState{Status: StatusSkipped})
	sess.UpdateStepState("g1b", StepState{Status: StatusSkipped})

	// Group status should be skipped
	items := wf.Items()
	if sess.ItemStatus(items[0]) != StatusSkipped {
		t.Errorf("Expected group status skipped, got %v", sess.ItemStatus(items[0]))
	}

	// Downstream should be allowed
	if !sess.IsStepRunnable(&wf, 2) {
		t.Error("Step 2 should be runnable after group is skipped")
	}
}

func TestParallelGroupMixedSuccessSkip(t *testing.T) {
	wf := Workflow{
		Name: "test",
		Steps: []StepOrGroup{
			{
				Group: &ParallelGroup{
					ID:   "g1",
					Name: "Group 1",
					Steps: []Step{
						{ID: "g1a", Name: "G1A", Script: "a.sh"},
						{ID: "g1b", Name: "G1B", Script: "b.sh"},
					},
				},
			},
			{Step: &Step{ID: "s2", Name: "Step 2", Script: "bar.sh"}},
		},
	}
	sess := NewSession(&wf, ".")

	// One success, one skip
	sess.UpdateStepState("g1a", StepState{Status: StatusSuccess})
	sess.UpdateStepState("g1b", StepState{Status: StatusSkipped})

	// Group status should be success
	items := wf.Items()
	if sess.ItemStatus(items[0]) != StatusSuccess {
		t.Errorf("Expected group status success, got %v", sess.ItemStatus(items[0]))
	}

	// Downstream should be allowed
	if !sess.IsStepRunnable(&wf, 2) {
		t.Error("Step 2 should be runnable after group completes with success+skip")
	}
}

func TestParallelGroupViewRendering(t *testing.T) {
	wf := Workflow{
		Name: "test",
		Steps: []StepOrGroup{
			{Step: &Step{ID: "s1", Name: "Step 1", Script: "foo.sh"}},
			{
				Group: &ParallelGroup{
					ID:   "g1",
					Name: "Group 1",
					Steps: []Step{
						{ID: "g1a", Name: "G1A", Script: "a.sh"},
						{ID: "g1b", Name: "G1B", Script: "b.sh"},
					},
				},
			},
			{Step: &Step{ID: "s2", Name: "Step 2", Script: "bar.sh"}},
		},
	}
	sess := NewSession(&wf, ".")
	m := initialModel(&wf, sess, ".")
	m.width = 100
	m.height = 30
	m.resizeViewports()

	view := m.View()
	if !strings.Contains(view.Content, "Group 1") {
		t.Errorf("View should contain group name 'Group 1', got:\n%s", view.Content)
	}
	if !strings.Contains(view.Content, "G1A") {
		t.Errorf("View should contain group step 'G1A', got:\n%s", view.Content)
	}
	if !strings.Contains(view.Content, "G1B") {
		t.Errorf("View should contain group step 'G1B', got:\n%s", view.Content)
	}
	if !strings.Contains(view.Content, "Step 1") {
		t.Errorf("View should contain 'Step 1', got:\n%s", view.Content)
	}
	if !strings.Contains(view.Content, "Step 2") {
		t.Errorf("View should contain 'Step 2', got:\n%s", view.Content)
	}
}

func TestParallelGroupCursorNavigation(t *testing.T) {
	wf := Workflow{
		Name: "test",
		Steps: []StepOrGroup{
			{Step: &Step{ID: "s1", Name: "Step 1", Script: "foo.sh"}},
			{
				Group: &ParallelGroup{
					ID:   "g1",
					Name: "Group 1",
					Steps: []Step{
						{ID: "g1a", Name: "G1A", Script: "a.sh"},
						{ID: "g1b", Name: "G1B", Script: "b.sh"},
					},
				},
			},
			{Step: &Step{ID: "s2", Name: "Step 2", Script: "bar.sh"}},
		},
	}
	sess := NewSession(&wf, ".")
	m := initialModel(&wf, sess, ".")

	// Initial cursor should be at step 0 (display index 0)
	if m.cursor != 0 {
		t.Errorf("Expected initial cursor=0, got %d", m.cursor)
	}

	// Navigate down: should go to group header (index 1)
	newM, _ := m.handleKeyMsg(tea.KeyPressMsg{Code: tea.KeyDown})
	if newM.cursor != 1 {
		t.Errorf("Expected cursor=1 after first down, got %d", newM.cursor)
	}

	// Navigate down: should go to group step g1a (index 2)
	newM, _ = newM.handleKeyMsg(tea.KeyPressMsg{Code: tea.KeyDown})
	if newM.cursor != 2 {
		t.Errorf("Expected cursor=2 after second down, got %d", newM.cursor)
	}

	// Navigate down: should go to group step g1b (index 3)
	newM, _ = newM.handleKeyMsg(tea.KeyPressMsg{Code: tea.KeyDown})
	if newM.cursor != 3 {
		t.Errorf("Expected cursor=3 after third down, got %d", newM.cursor)
	}

	// Navigate down: should go to step s2 (index 4)
	newM, _ = newM.handleKeyMsg(tea.KeyPressMsg{Code: tea.KeyDown})
	if newM.cursor != 4 {
		t.Errorf("Expected cursor=4 after fourth down, got %d", newM.cursor)
	}

	// Navigate up: should go back to g1b
	newM, _ = newM.handleKeyMsg(tea.KeyPressMsg{Code: tea.KeyUp})
	if newM.cursor != 3 {
		t.Errorf("Expected cursor=3 after up, got %d", newM.cursor)
	}
}

func TestParallelGroupCanRunOnHeader(t *testing.T) {
	wf := Workflow{
		Name: "test",
		Steps: []StepOrGroup{
			{Step: &Step{ID: "s1", Name: "Step 1", Script: "foo.sh"}},
			{
				Group: &ParallelGroup{
					ID:   "g1",
					Name: "Group 1",
					Steps: []Step{
						{ID: "g1a", Name: "G1A", Script: "a.sh"},
						{ID: "g1b", Name: "G1B", Script: "b.sh"},
					},
				},
			},
		},
	}
	sess := NewSession(&wf, ".")
	m := initialModel(&wf, sess, ".")

	// Set cursor on group header
	m.cursor = 1

	// Group should not be runnable yet because step 1 is not complete
	if m.canRun() {
		t.Error("canRun should be false on group header when previous step is not complete")
	}

	// Complete step 1
	sess.UpdateStepState("s1", StepState{Status: StatusSuccess})

	// Now group should be runnable from the header
	if !m.canRun() {
		t.Error("canRun should be true on group header after previous step completes")
	}

	// Run one step in the group
	sess.UpdateStepState("g1a", StepState{Status: StatusRunning})

	// Group is still runnable from header because g1b is pending
	if !m.canRun() {
		t.Error("canRun should be true on group header when some steps are still runnable")
	}

	// Complete both group steps
	sess.UpdateStepState("g1a", StepState{Status: StatusSuccess})
	sess.UpdateStepState("g1b", StepState{Status: StatusSuccess})

	// Group should be runnable for rerun
	if !m.canRun() {
		t.Error("canRun should be true on group header for rerun")
	}
}

func TestParallelGroupCanRunOnIndividualStep(t *testing.T) {
	wf := Workflow{
		Name: "test",
		Steps: []StepOrGroup{
			{Step: &Step{ID: "s1", Name: "Step 1", Script: "foo.sh"}},
			{
				Group: &ParallelGroup{
					ID:   "g1",
					Name: "Group 1",
					Steps: []Step{
						{ID: "g1a", Name: "G1A", Script: "a.sh"},
						{ID: "g1b", Name: "G1B", Script: "b.sh"},
					},
				},
			},
		},
	}
	sess := NewSession(&wf, ".")
	m := initialModel(&wf, sess, ".")

	// Set cursor on group step g1a
	m.cursor = 2

	// Complete step 1
	sess.UpdateStepState("s1", StepState{Status: StatusSuccess})

	// Individual step should be runnable
	if !m.canRun() {
		t.Error("canRun should be true on individual group step after previous step completes")
	}

	// Complete g1a
	sess.UpdateStepState("g1a", StepState{Status: StatusSuccess})

	// g1a should be runnable for rerun
	if !m.canRun() {
		t.Error("canRun should be true on individual step for rerun")
	}
}

func TestParallelGroupCanSkip(t *testing.T) {
	wf := Workflow{
		Name: "test",
		Steps: []StepOrGroup{
			{
				Group: &ParallelGroup{
					ID:   "g1",
					Name: "Group 1",
					Steps: []Step{
						{ID: "g1a", Name: "G1A", Script: "a.sh"},
						{ID: "g1b", Name: "G1B", Script: "b.sh"},
					},
				},
			},
		},
	}
	sess := NewSession(&wf, ".")
	m := initialModel(&wf, sess, ".")

	// Set cursor on group header
	m.cursor = 0

	// Group should be skippable
	if !m.canSkip() {
		t.Error("canSkip should be true on group header with pending steps")
	}

	// Skip one step
	sess.UpdateStepState("g1a", StepState{Status: StatusSkipped})

	// Group should still be skippable
	if !m.canSkip() {
		t.Error("canSkip should be true on group header when some steps are still pending")
	}

	// Skip all steps
	sess.UpdateStepState("g1b", StepState{Status: StatusSkipped})

	// Group should no longer be skippable
	if m.canSkip() {
		t.Error("canSkip should be false on group header when all steps are skipped")
	}
}

func TestParallelGroupAutoRun(t *testing.T) {
	if os.Getenv("SKIP_SHELL_TESTS") != "" {
		t.Skip("skipping shell-based auto-run chain test")
	}

	cwd := t.TempDir()
	script1 := filepath.Join(cwd, "step1.sh")
	script2 := filepath.Join(cwd, "step2.sh")
	script3 := filepath.Join(cwd, "step3.sh")
	if err := os.WriteFile(script1, []byte("#!/bin/sh\necho step1\n"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(script2, []byte("#!/bin/sh\necho step2\n"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(script3, []byte("#!/bin/sh\necho step3\n"), 0755); err != nil {
		t.Fatal(err)
	}

	wf := Workflow{
		Name: "test",
		Steps: []StepOrGroup{
			{Step: &Step{ID: "s1", Name: "Step 1", Script: "step1.sh", AutoRun: true}},
			{
				Group: &ParallelGroup{
					ID:   "g1",
					Name: "Group 1",
					Steps: []Step{
						{ID: "g1a", Name: "G1A", Script: "step2.sh", AutoRun: true},
						{ID: "g1b", Name: "G1B", Script: "step3.sh", AutoRun: true},
					},
				},
			},
		},
	}
	sess := NewSession(&wf, cwd)
	m := initialModel(&wf, sess, cwd)
	m.width = 100
	m.height = 30
	m.resizeViewports()

	// Simulate step 1 finishing while auto-run is active
	m.autoRun = true
	m.liveOutputs["s1"] = &liveOutput{stdout: []byte("step1 output\n")}

	newModel, _ := m.Update(shellDoneMsg{stepID: "s1", status: StatusSuccess, exitCode: 0})
	newM := newModel.(model)

	// Cursor should be on group header
	if newM.cursor != 1 {
		t.Errorf("Expected cursor=1 on group header, got %d", newM.cursor)
	}
	if !newM.autoRun {
		t.Error("Expected autoRun=true after chaining to group")
	}
	if sess.StepStates["g1a"].Status != StatusRunning {
		t.Errorf("Expected g1a status=running, got %v", sess.StepStates["g1a"].Status)
	}
	if sess.StepStates["g1b"].Status != StatusRunning {
		t.Errorf("Expected g1b status=running, got %v", sess.StepStates["g1b"].Status)
	}
}

func TestParallelGroupAutoRunChainsAfterGroupComplete(t *testing.T) {
	cwd := t.TempDir()
	script1 := filepath.Join(cwd, "step1.sh")
	script2 := filepath.Join(cwd, "step2.sh")
	script3 := filepath.Join(cwd, "step3.sh")
	script4 := filepath.Join(cwd, "step4.sh")
	if err := os.WriteFile(script1, []byte("#!/bin/sh\necho step1\n"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(script2, []byte("#!/bin/sh\necho step2\n"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(script3, []byte("#!/bin/sh\necho step3\n"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(script4, []byte("#!/bin/sh\necho step4\n"), 0755); err != nil {
		t.Fatal(err)
	}

	wf := Workflow{
		Name: "test",
		Steps: []StepOrGroup{
			{Step: &Step{ID: "s1", Name: "Step 1", Script: "step1.sh", AutoRun: true}},
			{
				Group: &ParallelGroup{
					ID:   "g1",
					Name: "Group 1",
					Steps: []Step{
						{ID: "g1a", Name: "G1A", Script: "step2.sh", AutoRun: true},
						{ID: "g1b", Name: "G1B", Script: "step3.sh", AutoRun: true},
					},
				},
			},
			{Step: &Step{ID: "s2", Name: "Step 2", Script: "step4.sh", AutoRun: true}},
		},
	}
	sess := NewSession(&wf, cwd)
	m := initialModel(&wf, sess, cwd)
	m.width = 100
	m.height = 30
	m.resizeViewports()

	m.autoRun = true

	// Simulate step 1 finishing
	m.liveOutputs["s1"] = &liveOutput{stdout: []byte("step1 output\n")}
	newModel, _ := m.Update(shellDoneMsg{stepID: "s1", status: StatusSuccess, exitCode: 0})
	newM := newModel.(model)

	// Simulate g1a finishing
	newM.liveOutputs["g1a"] = &liveOutput{stdout: []byte("g1a output\n")}
	newModel, _ = newM.Update(shellDoneMsg{stepID: "g1a", status: StatusSuccess, exitCode: 0})
	newM = newModel.(model)

	// Group is not complete yet, autoRun should still be active
	if !newM.autoRun {
		t.Error("Expected autoRun=true after first group step completes")
	}
	if sess.StepStates["s2"].Status != StatusPending {
		t.Errorf("Expected s2 status=pending, got %v", sess.StepStates["s2"].Status)
	}

	// Simulate g1b finishing
	newM.liveOutputs["g1b"] = &liveOutput{stdout: []byte("g1b output\n")}
	newModel, _ = newM.Update(shellDoneMsg{stepID: "g1b", status: StatusSuccess, exitCode: 0})
	newM = newModel.(model)

	// Now group is complete, autoRun should advance to s2
	if !newM.autoRun {
		t.Error("Expected autoRun=true after group completes, advancing to s2")
	}
	if newM.cursor != 4 {
		t.Errorf("Expected cursor=4 on step s2, got %d", newM.cursor)
	}
	if sess.StepStates["s2"].Status != StatusRunning {
		t.Errorf("Expected s2 status=running, got %v", sess.StepStates["s2"].Status)
	}
}

func TestParallelGroupAutoRunStopsOnGroupFailure(t *testing.T) {
	cwd := t.TempDir()
	script1 := filepath.Join(cwd, "step1.sh")
	script2 := filepath.Join(cwd, "step2.sh")
	script3 := filepath.Join(cwd, "step3.sh")
	if err := os.WriteFile(script1, []byte("#!/bin/sh\necho step1\n"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(script2, []byte("#!/bin/sh\necho step2\n"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(script3, []byte("#!/bin/sh\necho step3\n"), 0755); err != nil {
		t.Fatal(err)
	}

	wf := Workflow{
		Name: "test",
		Steps: []StepOrGroup{
			{Step: &Step{ID: "s1", Name: "Step 1", Script: "step1.sh", AutoRun: true}},
			{
				Group: &ParallelGroup{
					ID:   "g1",
					Name: "Group 1",
					Steps: []Step{
						{ID: "g1a", Name: "G1A", Script: "step2.sh", AutoRun: true},
						{ID: "g1b", Name: "G1B", Script: "step3.sh", AutoRun: true},
					},
				},
			},
			{Step: &Step{ID: "s2", Name: "Step 2", Script: "step4.sh", AutoRun: true}},
		},
	}
	sess := NewSession(&wf, cwd)
	m := initialModel(&wf, sess, cwd)
	m.width = 100
	m.height = 30
	m.resizeViewports()

	m.autoRun = true

	// Simulate step 1 finishing
	m.liveOutputs["s1"] = &liveOutput{stdout: []byte("step1 output\n")}
	newModel, _ := m.Update(shellDoneMsg{stepID: "s1", status: StatusSuccess, exitCode: 0})
	newM := newModel.(model)

	// Simulate g1a failing
	newM.liveOutputs["g1a"] = &liveOutput{stdout: []byte("g1a output\n")}
	newModel, _ = newM.Update(shellDoneMsg{stepID: "g1a", status: StatusFailed, exitCode: 1})
	newM = newModel.(model)

	// Group is not complete yet (g1b may still be running), autoRun should still be active
	if !newM.autoRun {
		t.Error("Expected autoRun=true while group is still in progress")
	}

	// Simulate g1b finishing (success)
	newM.liveOutputs["g1b"] = &liveOutput{stdout: []byte("g1b output\n")}
	newModel, _ = newM.Update(shellDoneMsg{stepID: "g1b", status: StatusSuccess, exitCode: 0})
	newM = newModel.(model)

	// Group is now complete but had a failure, autoRun should stop
	if newM.autoRun {
		t.Error("Expected autoRun=false after group completes with failure")
	}
	if sess.StepStates["s2"].Status != StatusPending {
		t.Errorf("Expected s2 status=pending, got %v", sess.StepStates["s2"].Status)
	}
}

func TestRunStepUpdatesStateOnMissingScript(t *testing.T) {
	wf := Workflow{
		Name: "test",
		Steps: []StepOrGroup{
			{Step: &Step{ID: "s1", Name: "Step 1", Script: "nonexistent.sh"}},
		},
	}
	sess := NewSession(&wf, ".")
	m := initialModel(&wf, sess, ".")
	m.width = 100
	m.height = 30
	m.resizeViewports()

	cmd := m.runStep(wf.FlatSteps()[0].Step)
	if cmd == nil {
		t.Fatal("Expected autoSave command, got nil")
	}

	// The state should be updated to failed
	state := sess.StepStates["s1"]
	if state.Status != StatusFailed {
		t.Errorf("Expected status=failed, got %v", state.Status)
	}
	if state.RunAt == "" {
		t.Error("Expected RunAt to be set after failed run")
	}
	if !strings.Contains(state.Stderr, "Script not found") {
		t.Errorf("Expected stderr to contain 'Script not found', got %q", state.Stderr)
	}
}

func TestRunStepUpdatesStateOnNonExecutableScript(t *testing.T) {
	tmpDir := t.TempDir()
	nonExec := filepath.Join(tmpDir, "nonexec.sh")
	if err := os.WriteFile(nonExec, []byte("#!/bin/sh\necho hello\n"), 0644); err != nil {
		t.Fatal(err)
	}

	wf := Workflow{
		Name: "test",
		Steps: []StepOrGroup{
			{Step: &Step{ID: "s1", Name: "Step 1", Script: nonExec}},
		},
	}
	sess := NewSession(&wf, tmpDir)
	m := initialModel(&wf, sess, tmpDir)
	m.width = 100
	m.height = 30
	m.resizeViewports()

	cmd := m.runStep(wf.FlatSteps()[0].Step)
	if cmd == nil {
		t.Fatal("Expected autoSave command, got nil")
	}

	state := sess.StepStates["s1"]
	if state.Status != StatusFailed {
		t.Errorf("Expected status=failed, got %v", state.Status)
	}
	if state.RunAt == "" {
		t.Error("Expected RunAt to be set after failed run")
	}
	if !strings.Contains(state.Stderr, "Script is not executable") {
		t.Errorf("Expected stderr to contain 'Script is not executable', got %q", state.Stderr)
	}
}

func TestSuccessfulStepCanBeRerun(t *testing.T) {
	wf := Workflow{
		Name: "test",
		Steps: []StepOrGroup{
			{Step: &Step{ID: "s1", Name: "Step 1", Script: "foo.sh"}},
			{Step: &Step{ID: "s2", Name: "Step 2", Script: "bar.sh"}},
		},
	}
	sess := NewSession(&wf, ".")

	// Complete step 0
	sess.UpdateStepState("s1", StepState{Status: StatusSuccess, RunAt: "2021-01-01T00:00:00Z"})

	// Step 0 should be runnable again (rerun)
	if !sess.IsStepRunnable(&wf, 0) {
		t.Error("Step 0 should be runnable again for rerun")
	}

	// Step 1 should also be runnable since step 0 is complete
	if !sess.IsStepRunnable(&wf, 1) {
		t.Error("Step 1 should be runnable after step 0 succeeds")
	}

	// Rerun should be possible - just verify the state transitions
	sess.UpdateStepState("s1", StepState{Status: StatusRunning})
	if sess.StepStates["s1"].Status != StatusRunning {
		t.Error("Step should be running after rerun")
	}
}

func TestRunOncePerSessionStillBlocksRerun(t *testing.T) {
	wf := Workflow{
		Name: "test",
		Steps: []StepOrGroup{
			{Step: &Step{ID: "s1", Name: "Step 1", Script: "foo.sh", RunOncePerSession: true}},
			{Step: &Step{ID: "s2", Name: "Step 2", Script: "bar.sh"}},
		},
	}
	sess := NewSession(&wf, ".")

	// Complete step 0 with run_once_per_session
	sess.UpdateStepState("s1", StepState{Status: StatusSuccess})

	// Step 0 should NOT be runnable again because run_once_per_session is set
	if sess.IsStepRunnable(&wf, 0) {
		t.Error("Step 0 should not be runnable again with run_once_per_session")
	}

	// Step 1 should be runnable
	if !sess.IsStepRunnable(&wf, 1) {
		t.Error("Step 1 should be runnable after step 0 succeeds")
	}
}

func TestParallelGroupManualRunRunsAllSteps(t *testing.T) {
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
		Steps: []StepOrGroup{
			{Step: &Step{ID: "s1", Name: "Step 1", Script: "step1.sh"}},
			{
				Group: &ParallelGroup{
					ID:   "g1",
					Name: "Group 1",
					Steps: []Step{
						{ID: "g1a", Name: "G1A", Script: "step1.sh"},
						{ID: "g1b", Name: "G1B", Script: "step2.sh", AutoRun: true},
					},
				},
			},
		},
	}
	sess := NewSession(&wf, cwd)
	m := initialModel(&wf, sess, cwd)

	// Complete step 1
	sess.UpdateStepState("s1", StepState{Status: StatusSuccess})

	// Set cursor on group header
	m.cursor = 1

	// Press r (not R) - autoRun should be false
	m.autoRun = false

	cmd := m.runCurrentStep()
	if cmd == nil {
		t.Fatal("Expected command from runCurrentStep")
	}

	// Both steps should be running (manual run runs all runnable steps)
	if sess.StepStates["g1a"].Status != StatusRunning {
		t.Errorf("Expected g1a status=running (manual run), got %v", sess.StepStates["g1a"].Status)
	}
	if sess.StepStates["g1b"].Status != StatusRunning {
		t.Errorf("Expected g1b status=running (manual run), got %v", sess.StepStates["g1b"].Status)
	}
}

func TestParallelGroupAutoRunOnlyRunsAutoRunSteps(t *testing.T) {
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
		Steps: []StepOrGroup{
			{Step: &Step{ID: "s1", Name: "Step 1", Script: "step1.sh"}},
			{
				Group: &ParallelGroup{
					ID:   "g1",
					Name: "Group 1",
					Steps: []Step{
						{ID: "g1a", Name: "G1A", Script: "step1.sh"},
						{ID: "g1b", Name: "G1B", Script: "step2.sh", AutoRun: true},
					},
				},
			},
		},
	}
	sess := NewSession(&wf, cwd)
	m := initialModel(&wf, sess, cwd)

	// Complete step 1
	sess.UpdateStepState("s1", StepState{Status: StatusSuccess})

	// Set cursor on group header
	m.cursor = 1

	// Press R (autoRun=true)
	m.autoRun = true

	cmd := m.runCurrentStep()
	if cmd == nil {
		t.Fatal("Expected command from runCurrentStep")
	}

	// Only g1b should be running (autoRun only runs auto_run steps)
	if sess.StepStates["g1a"].Status != StatusPending {
		t.Errorf("Expected g1a status=pending (autoRun filtered), got %v", sess.StepStates["g1a"].Status)
	}
	if sess.StepStates["g1b"].Status != StatusRunning {
		t.Errorf("Expected g1b status=running (autoRun step), got %v", sess.StepStates["g1b"].Status)
	}
}

func TestParallelGroupAutoRunChainOnlyRunsAutoRunSteps(t *testing.T) {
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
		Steps: []StepOrGroup{
			{Step: &Step{ID: "s1", Name: "Step 1", Script: "step1.sh", AutoRun: true}},
			{
				Group: &ParallelGroup{
					ID:   "g1",
					Name: "Group 1",
					Steps: []Step{
						{ID: "g1a", Name: "G1A", Script: "step1.sh"},
						{ID: "g1b", Name: "G1B", Script: "step2.sh", AutoRun: true},
					},
				},
			},
		},
	}
	sess := NewSession(&wf, cwd)
	m := initialModel(&wf, sess, cwd)
	m.width = 100
	m.height = 30
	m.resizeViewports()

	m.autoRun = true

	// Simulate step 1 finishing
	m.liveOutputs["s1"] = &liveOutput{stdout: []byte("step1 output\n")}
	newModel, _ := m.Update(shellDoneMsg{stepID: "s1", status: StatusSuccess, exitCode: 0})
	newM := newModel.(model)

	// Cursor should be on group header
	if newM.cursor != 1 {
		t.Errorf("Expected cursor=1 on group header, got %d", newM.cursor)
	}
	if !newM.autoRun {
		t.Error("Expected autoRun=true after chaining to group")
	}
	// g1a should NOT be running (no auto_run)
	if sess.StepStates["g1a"].Status != StatusPending {
		t.Errorf("Expected g1a status=pending (autoRun chain filtered), got %v", sess.StepStates["g1a"].Status)
	}
	// g1b should be running (auto_run)
	if sess.StepStates["g1b"].Status != StatusRunning {
		t.Errorf("Expected g1b status=running (autoRun chain), got %v", sess.StepStates["g1b"].Status)
	}
}

func TestParallelGroupAutoRunChainStopsWhenNoAutoRunSteps(t *testing.T) {
	wf := Workflow{
		Name: "test",
		Steps: []StepOrGroup{
			{Step: &Step{ID: "s1", Name: "Step 1", Script: "foo.sh", AutoRun: true}},
			{
				Group: &ParallelGroup{
					ID:   "g1",
					Name: "Group 1",
					Steps: []Step{
						{ID: "g1a", Name: "G1A", Script: "foo.sh"},
						{ID: "g1b", Name: "G1B", Script: "foo.sh"},
					},
				},
			},
			{Step: &Step{ID: "s2", Name: "Step 2", Script: "bar.sh", AutoRun: true}},
		},
	}
	sess := NewSession(&wf, ".")
	m := initialModel(&wf, sess, ".")
	m.width = 100
	m.height = 30
	m.resizeViewports()

	m.autoRun = true

	// Simulate step 1 finishing
	m.liveOutputs["s1"] = &liveOutput{stdout: []byte("step1 output\n")}
	newModel, _ := m.Update(shellDoneMsg{stepID: "s1", status: StatusSuccess, exitCode: 0})
	newM := newModel.(model)

	// No auto_run steps in group, autoRun should stop
	if newM.autoRun {
		t.Error("Expected autoRun=false when no auto_run steps in group")
	}
	// Group steps should remain pending
	if sess.StepStates["g1a"].Status != StatusPending {
		t.Errorf("Expected g1a status=pending, got %v", sess.StepStates["g1a"].Status)
	}
	if sess.StepStates["g1b"].Status != StatusPending {
		t.Errorf("Expected g1b status=pending, got %v", sess.StepStates["g1b"].Status)
	}
	// Step 2 should NOT have been started
	if sess.StepStates["s2"].Status != StatusPending {
		t.Errorf("Expected s2 status=pending, got %v", sess.StepStates["s2"].Status)
	}
}
