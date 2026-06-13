package main

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestRunnerBasicOutput(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("skipping shell runner tests on windows")
	}

	script := filepath.Join(t.TempDir(), "script.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\necho 'hello stdout'\necho 'hello stderr' >&2\nexit 0\n"), 0755); err != nil {
		t.Fatal(err)
	}

	step := Step{ID: "test", Script: script}
	runner := newStepRunner(step, filepath.Dir(script), script, nil)

	var gotStdout []string
	var gotStderr []string
	var done *shellDoneMsg

	for done == nil {
		cmd := runner.NextCmd()
		if cmd == nil {
			t.Fatal("NextCmd returned nil unexpectedly")
		}
		msg := cmd()
		switch m := msg.(type) {
		case shellStdoutMsg:
			gotStdout = append(gotStdout, m.line)
		case shellStderrMsg:
			gotStderr = append(gotStderr, m.line)
		case shellDoneMsg:
			done = &m
		default:
			t.Fatalf("unexpected message type: %T", msg)
		}
	}

	if len(gotStdout) != 1 || gotStdout[0] != "hello stdout\n" {
		t.Errorf("unexpected stdout: %v", gotStdout)
	}
	if len(gotStderr) != 1 || gotStderr[0] != "hello stderr\n" {
		t.Errorf("unexpected stderr: %v", gotStderr)
	}
	if done == nil {
		t.Fatal("expected shellDoneMsg")
	}
	if done.status != StatusSuccess {
		t.Errorf("expected status success, got %v", done.status)
	}
	if done.exitCode != 0 {
		t.Errorf("expected exit code 0, got %d", done.exitCode)
	}
}

func TestRunnerExitFailure(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("skipping shell runner tests on windows")
	}

	script := filepath.Join(t.TempDir(), "script.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\necho 'fail msg'\nexit 42\n"), 0755); err != nil {
		t.Fatal(err)
	}

	step := Step{ID: "fail", Script: script}
	runner := newStepRunner(step, filepath.Dir(script), script, nil)

	var done *shellDoneMsg
	for done == nil {
		cmd := runner.NextCmd()
		if cmd == nil {
			t.Fatal("NextCmd returned nil unexpectedly")
		}
		msg := cmd()
		switch m := msg.(type) {
		case shellDoneMsg:
			done = &m
		case shellStdoutMsg, shellStderrMsg:
			// ignore
		default:
			t.Fatalf("unexpected message type: %T", msg)
		}
	}

	if done.status != StatusFailed {
		t.Errorf("expected status failed, got %v", done.status)
	}
	if done.exitCode != 42 {
		t.Errorf("expected exit code 42, got %d", done.exitCode)
	}
}

func TestRunnerDrain(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("skipping shell runner tests on windows")
	}

	script := filepath.Join(t.TempDir(), "script.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\necho 'a'\necho 'b'\necho 'c'\nexit 0\n"), 0755); err != nil {
		t.Fatal(err)
	}

	step := Step{ID: "drain", Script: script}
	runner := newStepRunner(step, filepath.Dir(script), script, nil)

	// Consume the first stdout message
	cmd := runner.NextCmd()
	msg := cmd()
	if _, ok := msg.(shellStdoutMsg); !ok {
		t.Fatalf("expected first message to be stdout, got %T", msg)
	}

	// Drain should return the remaining lines
	stdout, stderr := runner.Drain()
	if len(stdout) != 2 || stdout[0] != "b\n" || stdout[1] != "c\n" {
		t.Errorf("unexpected drained stdout: %v", stdout)
	}
	if len(stderr) != 0 {
		t.Errorf("unexpected drained stderr: %v", stderr)
	}
}

func TestRunnerStop(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("skipping shell runner tests on windows")
	}

	script := filepath.Join(t.TempDir(), "script.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nsleep 5\n"), 0755); err != nil {
		t.Fatal(err)
	}

	step := Step{ID: "stop", Script: script}
	runner := newStepRunner(step, filepath.Dir(script), script, nil)

	// Give the goroutine a moment to start
	time.Sleep(50 * time.Millisecond)

	runner.Stop()

	var done *shellDoneMsg
	for done == nil {
		cmd := runner.NextCmd()
		if cmd == nil {
			t.Fatal("NextCmd returned nil unexpectedly")
		}
		msg := cmd()
		switch m := msg.(type) {
		case shellDoneMsg:
			done = &m
		case shellStdoutMsg, shellStderrMsg:
			// ignore
		default:
			t.Fatalf("unexpected message type: %T", msg)
		}
	}

	if done.status != StatusFailed {
		t.Errorf("expected status failed after stop, got %v", done.status)
	}
}

func TestRunnerManyLines(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("skipping shell runner tests on windows")
	}

	script := filepath.Join(t.TempDir(), "script.sh")
	content := "#!/bin/sh\nfor i in $(seq 1 500); do echo \"line $i\"; done\nexit 0\n"
	if err := os.WriteFile(script, []byte(content), 0755); err != nil {
		t.Fatal(err)
	}

	step := Step{ID: "many", Script: script}
	runner := newStepRunner(step, filepath.Dir(script), script, nil)

	var count int
	var done *shellDoneMsg
	for done == nil {
		cmd := runner.NextCmd()
		if cmd == nil {
			t.Fatal("NextCmd returned nil unexpectedly")
		}
		msg := cmd()
		switch m := msg.(type) {
		case shellStdoutMsg:
			count++
		case shellStderrMsg:
			// ignore
		case shellDoneMsg:
			done = &m
		default:
			t.Fatalf("unexpected message type: %T", msg)
		}
	}

	if done == nil || done.status != StatusSuccess {
		t.Fatalf("expected success, got %v", done)
	}
	if count != 500 {
		t.Errorf("expected 500 stdout lines, got %d", count)
	}
}

func TestRunnerLongLine(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("skipping shell runner tests on windows")
	}

	script := filepath.Join(t.TempDir(), "script.sh")
	longLine := make([]byte, 200*1024)
	for i := range longLine {
		longLine[i] = 'x'
	}
	content := "#!/bin/sh\nprintf '%s\n' '" + string(longLine) + "'\nexit 0\n"
	if err := os.WriteFile(script, []byte(content), 0755); err != nil {
		t.Fatal(err)
	}

	step := Step{ID: "long", Script: script}
	runner := newStepRunner(step, filepath.Dir(script), script, nil)

	var line string
	var done *shellDoneMsg
	for done == nil {
		cmd := runner.NextCmd()
		if cmd == nil {
			t.Fatal("NextCmd returned nil unexpectedly")
		}
		msg := cmd()
		switch m := msg.(type) {
		case shellStdoutMsg:
			line = m.line
		case shellStderrMsg:
			// ignore
		case shellDoneMsg:
			done = &m
		default:
			t.Fatalf("unexpected message type: %T", msg)
		}
	}

	if done == nil || done.status != StatusSuccess {
		t.Fatalf("expected success, got %v", done)
	}
	if len(line) != len(longLine)+1 {
		t.Errorf("expected line length %d, got %d", len(longLine)+1, len(line))
	}
}

func TestRunnerScannerOverflow(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("skipping shell runner tests on windows")
	}

	script := filepath.Join(t.TempDir(), "script.sh")
	// Create a line slightly longer than the 1MB scanner buffer
	longLine := make([]byte, 1024*1024+100)
	for i := range longLine {
		longLine[i] = 'x'
	}
	content := "#!/bin/sh\nprintf '%s\n' '" + string(longLine) + "'\nexit 0\n"
	if err := os.WriteFile(script, []byte(content), 0755); err != nil {
		t.Fatal(err)
	}

	step := Step{ID: "overflow", Script: script}
	runner := newStepRunner(step, filepath.Dir(script), script, nil)

	var done *shellDoneMsg
	for done == nil {
		cmd := runner.NextCmd()
		if cmd == nil {
			t.Fatal("NextCmd returned nil unexpectedly")
		}
		msg := cmd()
		switch m := msg.(type) {
		case shellDoneMsg:
			done = &m
		default:
			// ignore stdout and stderr
		}
	}

	if done == nil || done.status != StatusSuccess {
		t.Fatalf("expected success, got %v", done)
	}
}
