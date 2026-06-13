# Plan: Add Auto-Run Step Configuration

## Context

Add an optional `auto_run` boolean field to workflow steps (default `false`). When a user starts a task with uppercase `"R"` (not lowercase `"r"`), the selected task runs, and then the TUI automatically chains through any subsequent tasks that have `auto_run=true`, stopping when it encounters a task with `auto_run=false` (or the end of the workflow).

## Open Questions

1. **Cursor behavior during auto-run chain**: Should the cursor move down to each auto-running step so the user sees its live output, or should the cursor stay on the original step? *Recommended: move cursor.*
2. **Visual indicator for auto-run steps**: Should the step list show an indicator (e.g., `⏵` or `▶`) for steps that have `auto_run=true`? *Recommended: add an icon, similar to the existing `run_once_per_session` `⊘` icon.*
3. **Chain stop conditions**: The spec says stop when `auto_run=false`. Should the chain also stop if a step fails, or if a step is not runnable (e.g., `run_once_per_session` already succeeded)? *Recommended: yes, stop on failure or non-runnable.*
4. **Footer help text**: Should we update the footer to show `R` as a new key binding? *Recommended: yes, e.g., `r run  R auto-run`.*

## Proposed Approach

1. **Data model** (`workflow.go`): Add `AutoRun bool` to `Step` with JSON tag `auto_run,omitempty`.
2. **UI model** (`app.go`): Add `autoRun bool` to `model` to track whether an auto-run chain is active.
3. **Key handling** (`app.go`): Add `case "R":` (works in Bubble Tea v2 because `msg.String()` returns `"R"` for Shift+R). Set `autoRun=true` and call `runCurrentStep()`, but only if `canRun()`.
4. **Chain logic** (`app.go` `shellDoneMsg` handler): After a step finishes, if `autoRun` is active and the step succeeded, look at the next step. If it exists, has `auto_run=true`, and is runnable, advance `cursor`, call `loadStepOutput()`, and return `runCurrentStep()`. Otherwise, set `autoRun=false`.
5. **Icons** (`app.go`): Update `runTypeIcon` or add a new indicator for auto-run steps.
6. **Footer** (`app.go` `View`): Update the footer help text to mention `R`.
7. **Tests**: Add tests for `auto_run` JSON loading and auto-run chain behavior.

## Files to modify

- `workflow.go` — add `AutoRun` field
- `app.go` — add `autoRun` flag, handle `R` key, chain logic in `shellDoneMsg`, icon, footer
- `app_test.go` — add tests for auto-run behavior
- `examples/` — optionally update an example to demonstrate `auto_run`

## Reuse

- `m.canRun()` — already checks `IsStepRunnable` and current step state
- `m.runCurrentStep()` — already validates script, sets up runner, returns command
- `m.loadStepOutput()` — already loads buffers and refreshes viewport
- `IsStepRunnable` in `session.go` — already enforces sequence + run-once rules
- `runTypeIcon` in `app.go` — pattern for step-type icons

## Steps

- [ ] 1. Add `AutoRun` to `Step` struct in `workflow.go`
- [ ] 2. Add `autoRun bool` to `model` struct in `app.go`
- [ ] 3. Handle `case "R"` in `handleKeyMsg` (set flag, call `runCurrentStep` if `canRun`)
- [ ] 4. Extend `shellDoneMsg` handler to chain to next auto-run step
- [ ] 5. Add auto-run visual indicator in step list
- [ ] 6. Update footer help text
- [ ] 7. Add tests for JSON loading and auto-run chain
- [ ] 8. Update example workflow to showcase `auto_run`

## Verification

- `go test -v` passes
- Manual test: run `examples/full-demo.json` with `R` on a step, observe chain behavior
