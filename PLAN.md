# Plan: Parallel Step Groups

## Context
The curre project currently runs steps sequentially. Each step unlocks only after the previous step succeeds or is skipped. The user wants to add **parallel step groups** — a way to define a set of steps that run simultaneously, with any subsequent steps waiting for the entire group to finish before proceeding.

## Key Code Findings

### Current Execution Model
- `Workflow` contains a flat `Steps []Step` array (`workflow.go`)
- `Session.IsStepRunnable` enforces sequential dependencies: step `i` requires step `i-1` to be `success` or `skipped` (`session.go:245-248`)
- The TUI model tracks a single active runner: `runner *stepRunner` and `currentStepID string` (`app.go:101-102`)
- Auto-run chains advance `cursor` by one and run the next step sequentially (`app.go:186-196`)

### Runner Architecture
- `stepRunner` spawns a single `exec.CommandContext` and streams stdout/stderr via channels (`runner.go:30-160`)
- `shellDoneMsg` carries the `stepID`, `exitCode`, and `status` back to the `Update` loop (`runner.go:23-28`)
- The `Update` loop receives `shellDoneMsg`, persists state, clears the runner, and optionally auto-runs the next step (`app.go:168-200`)

### Rendering
- `renderStepListContent` iterates `m.workflow.Steps` and renders one line per step (`app.go:393-427`)
- Step status icons are rendered per step: `○ pending`, `● running`, `✓ done`, `✗ failed`, `⊘ skipped` (`app.go:429-450`)
- `loadStepOutput` and `handleLiveOutput` assume a single running step at a time when displaying live buffers (`app.go:492-520`)

## Design Decisions

### 1. JSON Schema — Option A (Group Wrapper) with Backward Compatibility

We will use a **union type** (`StepOrGroup`) so the `steps` array can contain either plain `Step` objects or `ParallelGroup` objects. This is **backward-compatible** via a custom `UnmarshalJSON` on `StepOrGroup` that inspects the raw JSON: if a `"steps"` key is present, it unmarshals as a `ParallelGroup`; otherwise, as a `Step`. Old workflow files (flat `[]Step`) continue to work without modification.

**Proposed schema:**
```json
{
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
}
```

**Internal structs:**
```go
type ParallelGroup struct {
    ID          string `json:"id"`
    Name        string `json:"name"`
    Description string `json:"description,omitempty"`
    Steps       []Step `json:"steps"`
}

type StepOrGroup struct {
    Step  *Step
    Group *ParallelGroup
}
```

### 2. Failure Behavior — Option B

If any step in a parallel group fails, the remaining steps in the group are allowed to continue running, but **all downstream steps are blocked**. The group's aggregate status is `failed` if any member fails, `success` if all members are `success` or `skipped`, `skipped` if all members are `skipped`, and `running` while any member is still running.

### 3. TUI Display — Option A (Group Header + Indented Steps)

The step list renders a **selectable group header line** (e.g., `╭─ Parallel: Test Suite`) followed by indented steps. The cursor (`> `) can land on **both group headers and individual steps**. Group-level status is shown in the header (e.g., `● running` or `✗ failed`). Navigation with `↑`/`↓` moves between all selectable items (headers and steps).

### 4. Individual Control

- **Navigate inside group**: Yes, cursor moves between group headers and individual steps.
- **Press `r` on a group header**: Starts **all runnable steps in the group simultaneously**. If the group is already running, `r` is a no-op.
- **Press `r` on an individual step inside a group**: Starts **that specific step only**. If the group is already running, `r` on a non-running step starts that step individually. If the step is already running, `r` is a no-op.
- **Press `d` to skip**: When on a group header, skips all remaining pending/failed steps in the group. When on an individual step, marks that specific step as `skipped`. If all steps in a group are skipped, the group completes and downstream unlocks.
- **Press `R` (auto-run)**: If the cursor is on a group header, all eligible steps in the group are started simultaneously and auto-run waits for the group to finish before advancing. If the cursor is on an individual step, it behaves like `r` on that step (starts it individually) and does not chain to the next workflow item — auto-run is only meaningful from a group header or sequential step.

### 5. `run_once` and `auto_run` within groups

These behave exactly as they do today, scoped to the individual step:
- A `run_once` step that is already `success` in the current session is skipped automatically when the group is started. The rest of the group still runs.
- `auto_run` steps within a group start automatically when the group is unlocked (either by the user pressing `R` or by advancing the auto-run chain).

## Proposed Approach

### Data Model Changes

1. **Add `ParallelGroup` and `StepOrGroup` to `workflow.go`**
2. **Implement custom `UnmarshalJSON` for `StepOrGroup`** for backward compatibility
3. **Add `Items()` and `FlatSteps()` helpers** to `Workflow` for navigating the nested structure
4. **Update `Validate()`** to check group IDs, nested step IDs, and ensure no empty groups

### Execution Model Changes

5. **Replace `runner *stepRunner` with `runners map[string]*stepRunner`** in `model` (`app.go`)
6. **Update `runCurrentStep`** to store the runner in the map and start the correct step
7. **Update `shellDoneMsg` handling** to:
   - Persist the finished step's state
   - Remove the runner from the map
   - Check if its group is now complete
   - If auto-run is active and the group is complete, advance to the next item
8. **Add `ItemStatus` and `IsGroupComplete` helpers to `Session`** (`session.go`)
9. **Rewrite `IsStepRunnable`** to check the previous *item* (group or step) rather than the previous step index

### TUI Changes

10. **Update `renderStepListContent`** to render group headers and indented steps
11. **Update cursor navigation** (`↑`/`↓`) to skip group headers and only land on steps
12. **Update `loadStepOutput` / `handleLiveOutput`** to support multiple simultaneous runners (already keyed by `stepID`, so mostly just remove the `currentStepID` check that gates display)
13. **Update `canRun`, `canSkip`, `autoRun` logic** to work with the flat step list

### Testing

14. **Add unit tests** for `StepOrGroup` JSON unmarshaling, `IsStepRunnable` with groups, `ItemStatus` aggregation
15. **Add integration tests** for parallel group execution in `app_test.go`
16. **Add `examples/parallel.json`** demonstrating a parallel group

## Files to Modify

| File | What to change |
|------|----------------|
| `workflow.go` | Add `ParallelGroup`, `StepOrGroup`, custom JSON unmarshal, `Items()`, `FlatSteps()`, update `Validate()` |
| `session.go` | Add `ItemStatus()`, `IsGroupComplete()`, rewrite `IsStepRunnable()` |
| `runner.go` | Minimal changes (already keyed by `stepID`) |
| `app.go` | Replace `runner` with `runners map`, update `runCurrentStep`, `shellDoneMsg`, rendering, navigation, `canRun`/`canSkip` |
| `runner_test.go` | Add parallel group tests |
| `app_test.go` | Add sequencing and TUI tests for groups |
| `examples/parallel.json` | New example workflow |

## Reuse — Existing Code to Leverage

- **`stepRunner`** (`runner.go`) — Already supports multiple concurrent instances because each instance is independent and identified by `stepID`. The `NextCmd()` / `shellDoneMsg` pattern scales naturally to multiple runners.
- **`liveOutputs map[string]*liveOutput`** (`app.go:99`) — Already stores per-step live output buffers. We just need to stop gating display on `m.currentStepID == stepID`.
- **`Session.StepStates`** (`session.go`) — Already a map keyed by `stepID`, so group membership doesn't affect state storage.
- **`shellStdoutMsg` / `shellStderrMsg` / `shellDoneMsg`** — Already carry `stepID`, so the `Update` loop can dispatch to the correct step without a global `currentStepID`.

## Verification

- `go test -v` — all existing tests pass
- New tests verify:
  - Old flat JSON still loads correctly
  - Mixed JSON (steps + groups) loads correctly
  - All steps in a group can be started simultaneously
  - Downstream steps wait for the entire group
  - If one group member fails, downstream is blocked
  - Group header renders; cursor skips headers
  - Auto-run starts all eligible group steps at once
  - `run_once` steps skip correctly inside a group
