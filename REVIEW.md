# Project Review: tui-workflow

**Overall Grade: B+**

A well-structured, functional Go TUI application for JSON-driven shell workflows. Clean architecture, good UX, and already migrated to the Charm v2 ecosystem. All tests pass and `go vet` is clean. However, there are several concurrency and reliability issues in the shell execution layer that need to be addressed before the project can be considered robust.

---

## Grade Breakdown by Category

| Category | Grade | Notes |
|----------|-------|-------|
| Architecture | A | Excellent separation of concerns across 5 files. |
| Correctness | C | Concurrency bugs in `runner.go` (deadlocks, race conditions). |
| Reliability | C | `bufio.Scanner` 64KB line limit, goroutine leaks on quit. |
| UX / Polish | A- | Thoughtful UI, minor issues with cursor positioning. |
| Test Coverage | B | Good unit tests, missing integration tests for runner. |
| Code Quality | B+ | Clean style, some magic numbers, minor layout fragility. |

---

## Critical Issues (Must Fix Before Release)

### 1. Deadlock Risk in `stepRunner` (runner.go)

**Severity: Critical**

The `stdoutChan`/`stderrChan` are buffered to 100. If a script produces >100 lines between `Update()` ticks, the runner goroutine blocks trying to send, and the TUI freezes. The `resultChan` is unbuffered, so the goroutine can deadlock after `cmd.Wait()` returns if `NextCmd()` is not called again.

**Evidence:**
```go
stdoutChan := make(chan string, 100)   // Too small
stderrChan := make(chan string, 100)   // Too small
resultChan := make(chan shellDoneMsg)  // Unbuffered = deadlock risk
```

**Fix:**
- Buffer `resultChan` with capacity 1.
- Increase stdout/stderr channel buffers to at least 1000, or use a non-blocking send pattern.

### 2. `bufio.Scanner` Line Length Limit

**Severity: Critical**

`bufio.Scanner` has a hard 64KB line limit. If a script outputs a line longer than 64KB (e.g., a large JSON blob without newlines), the scanner silently truncates the line and logs an error to stderr. This is invisible to the user.

**Evidence:**
```go
scanner := bufio.NewScanner(stdout)
for scanner.Scan() {
    stdoutChan <- scanner.Text() + "\n"
}
```

**Fix:**
- Increase scanner buffer, or switch to `bufio.Reader.ReadString('\n')`.

### 3. Race Condition Between `Drain()` and `NextCmd()`

**Severity: Critical**

Both `Drain()` and `NextCmd()` read from `stdoutChan`/`stderrChan`. If a `shellDoneMsg` arrives, `Update()` calls `Drain()` to collect remaining output. But `NextCmd()` may still be in flight (e.g., if a `tea.Msg` is already queued). Both will try to read from the same channels, causing a race. The `goto` pattern in `Drain()` is fragile.

**Evidence:**
```go
func (r *stepRunner) Drain() (stdout, stderr []string) {
    for {
        select {
        case line := <-r.stdoutChan:
            stdout = append(stdout, line)
        default:
            goto drainStderr   // Fragile control flow
        }
    }
```

**Fix:**
- Replace the `goto` with a function.
- Ensure `Drain()` is only called once and channels are closed after draining.

### 4. Goroutine Leak on Quit

**Severity: High**

If the user presses `q` while a step is running, the TUI quits but the `stepRunner` goroutine is still alive. It will eventually finish `cmd.Wait()` and try to send on `resultChan`, but nobody is reading. If the channel is unbuffered, the goroutine leaks forever.

**Fix:**
- Add a `cancel` channel or use `context.Context` to signal the runner to abort.
- Kill the `exec.Cmd` process on quit.

---

## High Issues (Should Fix Soon)

### 5. `go.mod` Declares Invalid Go Version

**Severity: High**

`go.mod` says `go 1.26.4`. Go 1.26 does not exist. The latest stable is 1.24. This will cause issues with some Go tooling and module resolution.

**Fix:**
- Change to `go 1.23` or whatever minimum version is actually required.

### 6. `Cursor()` Positioning is Brittle

**Severity: High**

The `Cursor()` method in `app.go` computes screen coordinates with hardcoded arithmetic (`+ 3`, `* 3`, `+ 1`). If `renderParamContent()` or `resizeViewports()` changes, the cursor will be misaligned.

**Evidence:**
```go
c.Y += paramLines + 3 + m.focusedParam*3
```

**Fix:**
- Extract layout constants into named constants, or compute offsets from the same helper functions used for rendering.

### 7. `refreshStdoutContent` Logic is Confusing

**Severity: Medium**

The condition `m.currentStepID != m.workflow.Steps[m.cursor].ID` is used to decide whether to render markdown. This means markdown is NOT re-rendered while the step is running (because `currentStepID == step.ID`). This is probably intentional, but the code is confusing and could break if the state machine changes.

**Fix:**
- Refactor into a named boolean with a comment, or use a helper method.

---

## Medium Issues (Nice to Fix)

### 8. No Error Visibility for `autoSave` Failures

**Severity: Medium**

If `SaveSession()` fails (e.g., disk full), an `errMsg` is sent to the model, but the user only sees it in the stderr pane. If the user is not focused on a step with stderr output, they won't notice.

**Fix:**
- Add a transient status bar or toast message for save errors.

### 9. `renderViewportContent` Bypasses Viewport API

**Severity: Medium**

For markdown output, the code manually slices lines based on `YOffset` and `Height` instead of using `viewport.Model.View()`. This works because lipgloss's `MaxWidth` is not ANSI-aware, but it couples the code to the viewport's internal representation.

**Fix:**
- Add a detailed comment explaining the workaround, and wrap it in a helper type for clarity.

### 10. Missing Runner Integration Tests

**Severity: Medium**

There are no tests for `stepRunner` or `buildParams`. The runner is the most complex and bug-prone part of the app.

**Fix:**
- Add tests that run a mock script (e.g., `echo hello` or `sleep 0.1`) and verify the message sequence.

### 11. Magic Numbers in Layout

**Severity: Low**

`app.go` contains many layout constants (`10`, `3`, `2`, `236`, etc.) without explanation.

**Fix:**
- Extract into named constants with comments.

---

## Low / Nit Issues

### 12. `go.sum` Could Be Tidied

**Severity: Low**

Run `go mod tidy` to ensure no stale entries.

### 13. `Init()` Returns `nil`

**Severity: Low**

Not a bug, but returning `nil` means the app does nothing on startup. If you ever want to add async loading, you'll need to change this.

### 14. `Output` Field Backward Compat

**Severity: Low**

The `GetOutput()` method parses an old combined format. This is fine, but the `Output` field itself should be marked deprecated in a comment.

---

## Summary of Required Fixes

| # | Issue | File | Severity |
|---|-------|------|----------|
| 1 | Deadlock: small channel buffers | `runner.go` | Critical |
| 2 | Deadlock: unbuffered resultChan | `runner.go` | Critical |
| 3 | `bufio.Scanner` 64KB limit | `runner.go` | Critical |
| 4 | Race: `Drain()` vs `NextCmd()` | `runner.go` | Critical |
| 5 | Goroutine leak on quit | `runner.go` | High |
| 6 | Invalid `go` version in `go.mod` | `go.mod` | High |
| 7 | Brittle cursor positioning | `app.go` | High |
| 8 | Confusing markdown refresh logic | `app.go` | Medium |
| 9 | Silent `autoSave` errors | `app.go` | Medium |
| 10 | Viewport bypass needs comment | `app.go` | Medium |
| 11 | No runner tests | `runner_test.go` | Medium |
| 12 | Magic numbers | `app.go` | Low |
| 13 | `go mod tidy` | `go.mod` | Low |
| 14 | `Output` field deprecation | `session.go` | Low |

---

## Recommended Priority Order

1. **Fix `runner.go` concurrency** (Issues 1–5). These are the most likely to cause real-world crashes.
2. **Fix `go.mod` version** (Issue 6). One-line change.
3. **Fix cursor positioning** (Issue 7). User-facing bug.
4. **Add runner tests** (Issue 11). Prevents regressions on the critical path.
5. **Clean up `app.go` logic** (Issues 8–10, 12). Polish.
6. **Final tidy** (Issues 13–14).
