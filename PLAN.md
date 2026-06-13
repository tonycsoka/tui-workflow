# TUI-Workflow: Comprehensive Bug-Fix Plan

## Context

This plan addresses all issues identified in the `REVIEW.md` graded audit. The top priority is fixing the `runner.go` concurrency bugs (deadlocks, race conditions, goroutine leaks) which are the only Critical-severity issues. Secondary items improve correctness, reliability, and code quality.

## Approach

1. **Fix `runner.go` first** — add `context.Context`, buffered channels, large scanner buffers, and clean shutdown. This is the highest-impact change.
2. **Fix `go.mod` version** — one-line change, prevents module tooling issues.
3. **Fix `app.go` cursor positioning** — extract layout constants and compute offsets from the same helpers used for rendering.
4. **Improve `app.go` clarity** — refactor `refreshStdoutContent` logic, add comments to `renderViewportContent`, surface `autoSave` errors.
5. **Add `runner_test.go`** — integration tests for the shell runner to prevent regressions.
6. **Final polish** — `go mod tidy`, deprecate `Output` field, extract magic numbers.

## Files to Modify

- `runner.go` — Critical concurrency fixes
- `app.go` — Cursor positioning, clarity, error visibility
- `session.go` — Deprecation comment for `Output` field
- `go.mod` — Fix `go` version
- `runner_test.go` — New file, integration tests
- `go.sum` — Updated via `go mod tidy`

## Reuse

- `tea.KeyMsg` handling in `app.go` — we only add to it, not replace.
- `Session.SaveSession` / `LoadSessionFromPath` — unchanged.
- `ResolveScriptPath` — unchanged.
- `buildParams` — unchanged.

---

## Steps

### Phase 1: `runner.go` Concurrency Fixes (Critical)

- [ ] **Add `context.Context` to `stepRunner`**
  - Add a `ctx context.Context` field and a `cancel context.CancelFunc` field.
  - In `newStepRunner`, create a cancellable context.
  - Pass `cmd.Dir` and `cmd.Cancel = func() error { return cmd.Process.Kill() }` (or use `cmd.Process.Signal(os.Interrupt)` then `Kill()` after timeout).
  - Actually, the idiomatic way is `cmd.Cancel = cancel` where `cancel` is the context cancel func. Or better: wrap the command with the context.
  - Simpler: store the `*exec.Cmd` in `stepRunner`, add a `Stop()` method that calls `cmd.Process.Kill()`.

- [ ] **Buffer channels properly**
  - `stdoutChan`: `make(chan string, 1000)`
  - `stderrChan`: `make(chan string, 1000)`
  - `resultChan`: `make(chan shellDoneMsg, 1)`

- [ ] **Fix `bufio.Scanner` line limit**
  - Allocate a `[]byte` buffer of size 1MB and call `scanner.Buffer(buf, cap(buf))` for both stdout and stderr scanners.

- [ ] **Clean up `Drain()`**
  - Remove the `goto` pattern. Replace with a helper `drainChan(ch chan string) []string`.
  - Ensure `Drain()` is only called once. After `Drain()`, close the channels (or just let the goroutine exit after sending the result).
  - The goroutine should close `stdoutChan` and `stderrChan` after `cmd.Wait()` returns and `wg.Wait()` completes. This makes `Drain()` a simple `for line := range ch` loop.

- [ ] **Prevent goroutine leak on quit**
  - Add `func (r *stepRunner) Stop() error` that kills the process.
  - In `app.go`, when `tea.Quit` is triggered, call `m.runner.Stop()` if `m.runner != nil`.
  - The goroutine should select on `ctx.Done()` or use `cmd.Cancel` to abort cleanly.

- [ ] **Update `NextCmd()` to handle closed channels**
  - If `stdoutChan` and `stderrChan` are closed, the goroutine may have already sent the result. `NextCmd()` should still read the result from `resultChan`.
  - Use a `select` with `case <-ctx.Done():` for cancellation, but since `resultChan` is buffered, the goroutine won't block.

### Phase 2: `go.mod` Version Fix (High)

- [ ] Change `go 1.26.4` to `go 1.23` (or whatever minimum is actually required). The project uses no generics or other 1.24+ features.

### Phase 3: `app.go` Cursor and Clarity (High / Medium)

- [ ] **Extract layout constants**
  - Define `const paramLabelHeight = 1`, `const paramInputHeight = 1`, `const paramSpacing = 1`, so `paramBlockHeight = 3` per parameter.
  - Define `const titleBarHeight = 1`, `const footerHeight = 1`.
  - Compute `paramYOffset` in a helper `func (m model) paramPaneYOffset() int`.

- [ ] **Fix `Cursor()`**
  - Replace the hardcoded `+ 3 + m.focusedParam*3` with a call to a layout helper that computes the exact Y position of the focused parameter input.
  - The helper should mirror the vertical layout logic in `renderParamContent` and `resizeViewports`.

- [ ] **Refactor `refreshStdoutContent`**
  - Extract `isRunningThisStep := m.currentStepID == step.ID`.
  - Add a comment: `// While a step is running, we render raw stdout/stderr so the user sees live output. After it finishes, we render markdown via glamour.`
  - The condition should read: `if !isRunningThisStep && step.OutputType == OutputMarkdown && stdoutStr != ""`.

- [ ] **Comment `renderViewportContent`**
  - Add a comment explaining why we bypass `viewport.View()` for markdown: `// lipgloss's MaxWidth truncation is not ANSI-aware; glamour already word-wraps at the correct width, so we manually slice the visible lines to avoid stripping ANSI codes.`

- [ ] **Surface `autoSave` errors**
  - Add a `saveErr string` field to the model.
  - In `renderTitle`, if `saveErr != ""`, render it in a red style appended to the title bar.
  - Clear `saveErr` after 3 seconds or on the next keypress.
  - Alternatively, append to the footer. Simpler: append to the title bar as a transient red segment.

### Phase 4: Tests (Medium)

- [ ] **Create `runner_test.go`**
  - Test `newStepRunner` with a script that prints `hello` and exits 0.
  - Verify the message sequence: `shellStdoutMsg{line: "hello\n"}`, then `shellDoneMsg{status: StatusSuccess, exitCode: 0}`.
  - Test with a script that exits non-zero.
  - Test with a script that outputs >100 lines to verify the buffer doesn't deadlock.
  - Test `Stop()` by starting a long-running `sleep` script and calling `Stop()`, then verifying the result is `StatusFailed`.
  - Test `Drain()` by running a script that prints, then checking that `Drain()` returns the remaining lines.

- [ ] **Update existing tests if needed**
  - `TestViewRendersSteps` and others should still pass after `Cursor()` changes.

### Phase 5: Polish (Low)

- [ ] **`go mod tidy`**
  - Run `go mod tidy` to clean `go.sum`.

- [ ] **Deprecate `Output` field in `session.go`**
  - Add comment: `// Output is deprecated. Use Stdout and Stderr instead. Kept for backward compatibility with sessions created before vX.Y.`

- [ ] **Extract magic numbers in `app.go`**
  - Replace `10`, `236`, `45`, etc. with named constants where appropriate.

- [ ] **`Init()` returns `tea.Batch` or `nil`**
  - Not a bug, but add a comment: `// Init returns no commands. If async loading is needed in the future, return a tea.Cmd here.`

---

## Verification

1. **Build**: `go build .` should succeed with zero warnings.
2. **Tests**: `go test -v ./...` should pass all 9+ tests (including new runner tests).
3. **Vet**: `go vet ./...` should be clean.
4. **Deadlock test**: Run `./tui-workflow examples/deploy.json`, start a step that produces a lot of output (e.g., `seq 1 10000`), verify the TUI does not freeze.
5. **Quit test**: Start a long-running step, press `q` — the TUI should quit immediately without hanging.
6. **Long line test**: Run a script that prints a single line >64KB, verify it appears complete.
7. **Markdown test**: Run `./tui-workflow examples/markdown.json`, run the markdown step, verify `PgUp`/`PgDown` scroll correctly.
8. **Cursor test**: Focus a parameter with `Tab`, verify the cursor appears at the correct position.
9. **Session error test**: Make `~/.local/share/tui-workflow/sessions` read-only, change a parameter, verify the error appears in the UI.

---

## Rollback Plan

- `runner.go` is the riskiest change. Keep a backup of the original `runner.go` logic.
- The channel and scanner changes are additive (larger buffers) and should not break existing behavior.
- `stepRunner.Stop()` is new; if it causes issues, it can be removed without affecting the happy path.
