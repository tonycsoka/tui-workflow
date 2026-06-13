package main

import (
	"bufio"
	"context"
	"errors"
	"os/exec"
	"sync"

	tea "charm.land/bubbletea/v2"
)

type shellStdoutMsg struct {
	line   string
	stepID string
}

type shellStderrMsg struct {
	line   string
	stepID string
}

type shellDoneMsg struct {
	stepID   string
	exitCode int
	status   StepStatus
}

type stepRunner struct {
	stdoutChan chan string
	stderrChan chan string
	resultChan chan shellDoneMsg
	stepID     string
	cancel     context.CancelFunc
	done       chan struct{}
	stopOnce   sync.Once
	drained    bool
}

func newStepRunner(step Step, workflowDir string, scriptPath string, params []string) *stepRunner {
	ctx, cancel := context.WithCancel(context.Background())
	stdoutChan := make(chan string, 1000)
	stderrChan := make(chan string, 1000)
	resultChan := make(chan shellDoneMsg, 1)
	done := make(chan struct{})

	cmd := exec.CommandContext(ctx, scriptPath, params...)
	cmd.Dir = workflowDir

	go func() {
		defer cancel()
		defer close(resultChan)

		stdout, err := cmd.StdoutPipe()
		if err != nil {
			resultChan <- shellDoneMsg{stepID: step.ID, exitCode: -1, status: StatusFailed}
			return
		}
		stderr, err := cmd.StderrPipe()
		if err != nil {
			resultChan <- shellDoneMsg{stepID: step.ID, exitCode: -1, status: StatusFailed}
			return
		}

		if err := cmd.Start(); err != nil {
			resultChan <- shellDoneMsg{stepID: step.ID, exitCode: -1, status: StatusFailed}
			return
		}

		var wg sync.WaitGroup
		wg.Add(2)

		go func() {
			defer wg.Done()
			scanner := bufio.NewScanner(stdout)
			buf := make([]byte, 1024*1024)
			scanner.Buffer(buf, cap(buf))
			for scanner.Scan() {
				select {
				case stdoutChan <- scanner.Text() + "\n":
				case <-done:
					return
				}
			}
			if err := scanner.Err(); err != nil {
				select {
				case <-done:
					// Suppress error due to forced stop
				default:
					logWarning("stdout scanner error: %v", err)
				}
			}
		}()

		go func() {
			defer wg.Done()
			scanner := bufio.NewScanner(stderr)
			buf := make([]byte, 1024*1024)
			scanner.Buffer(buf, cap(buf))
			for scanner.Scan() {
				select {
				case stderrChan <- scanner.Text() + "\n":
				case <-done:
					return
				}
			}
			if err := scanner.Err(); err != nil {
				select {
				case <-done:
					// Suppress error due to forced stop
				default:
					logWarning("stderr scanner error: %v", err)
				}
			}
		}()

		if err := cmd.Wait(); err != nil {
			exitCode := -1
			var exitErr *exec.ExitError
			if errors.As(err, &exitErr) {
				exitCode = exitErr.ExitCode()
			}
			wg.Wait()
			resultChan <- shellDoneMsg{stepID: step.ID, exitCode: exitCode, status: StatusFailed}
		} else {
			wg.Wait()
			resultChan <- shellDoneMsg{stepID: step.ID, exitCode: 0, status: StatusSuccess}
		}
	}()

	return &stepRunner{
		stdoutChan: stdoutChan,
		stderrChan: stderrChan,
		resultChan: resultChan,
		stepID:     step.ID,
		cancel:     cancel,
		done:       done,
		stopOnce:   sync.Once{},
		drained:    false,
	}
}

func (r *stepRunner) NextCmd() tea.Cmd {
	if r == nil {
		return nil
	}
	return func() tea.Msg {
		select {
		case line := <-r.stdoutChan:
			return shellStdoutMsg{line: line, stepID: r.stepID}
		case line := <-r.stderrChan:
			return shellStderrMsg{line: line, stepID: r.stepID}
		case result := <-r.resultChan:
			return result
		}
	}
}

// Stop cancels the runner's context, killing the underlying process.
func (r *stepRunner) Stop() {
	if r == nil {
		return
	}
	r.stopOnce.Do(func() {
		r.cancel()
		close(r.done)
	})
}

// Drain returns any remaining output in the buffers without blocking.
// It is safe to call multiple times; subsequent calls are no-ops.
func (r *stepRunner) Drain() (stdout, stderr []string) {
	if r == nil || r.drained {
		return nil, nil
	}
	r.drained = true
	return drainChan(r.stdoutChan), drainChan(r.stderrChan)
}

func drainChan(ch chan string) []string {
	var lines []string
	for {
		select {
		case line := <-ch:
			lines = append(lines, line)
		default:
			return lines
		}
	}
}

func buildParams(step Step, m *model) []string {
	var params []string
	for _, name := range step.Params {
		val := m.session.GetParameterValue(name, m.workflow)
		params = append(params, val)
	}
	return params
}
