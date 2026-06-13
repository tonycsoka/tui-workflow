# Plan: Tabbed Stdout/Stderr and Parameter Validation

## Context

The `tui-workflow` app currently renders three vertical panes on the right side:
1. Parameters
2. Stdout  
3. Stderr

This splits the available vertical space, making it hard to see full output. The user wants:
1. **Tabbed stdout/stderr panes** — so they share the same vertical space and can be switched between, giving more room to whichever is active.
2. **Block runs if any parameter is not set** — prevent a step from running when a required parameter is empty/unset.

## Approach

### Tabbed stdout/stderr

- Replace the two separate stdout/stderr vertical panes with a single **output pane** that has a tab bar at the top (`Stdout | Stderr`).
- Add an `outputTab` field to the model (`0` = stdout, `1` = stderr).
- Use **left/right arrow keys** to switch tabs when not editing parameters. Wrap around at the edges.
- Give both viewports the full available height (no longer split 50/50). Only the active tab's viewport content is rendered in the output pane.
- Call `loadStepOutput()` after switching tabs so the newly visible viewport is refreshed from the live buffer.

### Parameter validation

- A parameter is "not set" if `GetParameterValue(name, wf)` returns an empty string — meaning **no user value and no default**.
- Add `allParamsSet()` to check every parameter in the workflow has a non-empty resolved value.
- Add `allParamsSet()` to `canRun()` so `r` and `R` are **completely blocked** for all steps until every workflow parameter is set.
- The footer will show `⚠ set all parameters to run` in place of the `r run  R auto-run` hint when parameters are missing.

## Files to Modify

- `app.go` — add `outputTab`, tab rendering, `allParamsSet()`, `canRun()` update, `resizeViewports()` refactor, left/right key handling
- `app_test.go` — add tab-switching tests and parameter-gating tests

## Steps

- [ ] Add `outputTab` field to `model` and `initialModel`
- [ ] Add `renderOutputTabs()` with active/inactive styles
- [ ] Refactor `View()` to render params pane + single output pane with tab bar
- [ ] Refactor `resizeViewports()` to allocate full remaining height to both viewports
- [ ] Add left/right arrow handling in `handleKeyMsg()` (wrap around, call `loadStepOutput()`)
- [ ] Add `allParamsSet()` method on `model`
- [ ] Update `canRun()` to require `allParamsSet()`
- [ ] Update footer rendering to warn when parameters are missing
- [ ] Add tests for tab switching and parameter validation gating

## Verification

- `go test ./...` should pass
- Run `go run . examples/deploy.json` and verify:
  - Left/right arrows switch between stdout and stderr tabs
  - Both tabs show current output when switching while a step is running
  - `r` and `R` are blocked when any parameter without a default is empty
  - Footer shows the parameter warning when a parameter is missing
