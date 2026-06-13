# TUI Workflow

A JSON-driven terminal UI for running sequenced, parameterised shell workflows.

> ⚠️ **Security Warning**: `tui-workflow` executes arbitrary shell scripts with your full user privileges. Only run workflow files from trusted sources.

## Features

- **JSON-driven workflows**: Define steps, parameters, and scripts in a simple JSON file.
- **Interactive parameter input**: Edit parameters in the TUI before running each step.
- **Sequential execution**: Steps unlock only after the previous step succeeds (or is skipped).
- **Parallel step groups**: Run a set of steps simultaneously, with downstream steps waiting for the entire group to finish.
- **Session persistence**: Auto-saved, directory-aware sessions with unique datetime-based names. Resume or switch between sessions.
- **Live output**: Stream stdout/stderr from scripts in real-time.
- **Markdown output**: Steps can render their output as styled markdown via glamour.
- **Run-type indicators**: Steps show icons indicating whether they're repeatable (↻), run-once (⊘), or auto-run (⏵).
- **Step info pane**: Shows description and last run time for the selected step.

## Installation

### From GitHub (latest)

```bash
go install github.com/tonycsoka/tui-workflow@latest
```

Then run it directly:

```bash
tui-workflow <workflow.json>
```

### From source

```bash
git clone https://github.com/tonycsoka/tui-workflow.git
cd tui-workflow
go build .
```

## Usage

```bash
./tui-workflow <workflow.json>
```

Example:

```bash
./tui-workflow examples/deploy.json
```

A comprehensive demo showing all features:

```bash
./tui-workflow examples/full-demo.json
```

## Workflow JSON Format

```json
{
  "name": "deploy",
  "description": "Deploy the application",
  "parameters": {
    "env": {
      "type": "string",
      "default": "dev",
      "description": "Target environment"
    }
  },
  "steps": [
    {
      "id": "build",
      "name": "Build",
      "script": "scripts/build.sh",
      "params": ["env"],
      "run_once_per_session": false,
      "description": "Build the application"
    },
    {
      "id": "deploy",
      "name": "Deploy",
      "script": "scripts/deploy.sh",
      "params": ["env"],
      "run_once_per_session": true,
      "description": "Deploy the application"
    }
  ]
}
```

### Field Reference

- `name` (string, required): Workflow name.
- `description` (string): Workflow description shown in the title bar.
- `parameters` (object): Global parameters available to all steps.
  - `type`: Parameter type (`string`).
  - `default`: Default value.
  - `description`: Human-readable description.
- `steps` (array, required): Each element is either a **step** or a **parallel group**.
  - **Step**:
    - `id`: Unique step identifier.
    - `name`: Display name.
    - `script`: Path to shell script (relative to workflow JSON or absolute).
    - `params`: Array of parameter names to pass as positional arguments to the script.
    - `run_once_per_session`: If `true`, the step is skipped if it already succeeded in the current session.
    - `auto_run`: If `true`, the step is automatically executed as part of an auto-run chain triggered by pressing `R`.
    - `output_type`: Set to `"markdown"` to render the step's stdout as styled markdown.
    - `description`: Description shown in the step info pane.
  - **Parallel Group**:
    - `id`: Unique group identifier.
    - `name`: Display name.
    - `description`: Description shown in the group info pane.
    - `steps`: Array of steps that run in parallel.

### Parallel Groups

Define a parallel group by nesting a `steps` array inside a group object:

```json
{
  "name": "parallel-demo",
  "steps": [
    {"id": "setup", "name": "Setup", "script": "scripts/setup.sh"},
    {
      "id": "tests",
      "name": "Run Tests",
      "description": "Run multiple tests in parallel",
      "steps": [
        {"id": "unit", "name": "Unit Tests", "script": "scripts/unit.sh"},
        {"id": "lint", "name": "Lint", "script": "scripts/lint.sh"},
        {"id": "typecheck", "name": "Type Check", "script": "scripts/typecheck.sh"}
      ]
    },
    {"id": "deploy", "name": "Deploy", "script": "scripts/deploy.sh"}
  ]
}
```

- All steps inside a group run simultaneously once the group is unlocked.
- Downstream steps wait until **every** step in the group finishes (success, failed, or skipped).
- If any step in a group fails, downstream steps are blocked, but remaining group steps continue running.
- `r` on a group header starts all runnable steps in the group.
- `r` on an individual step inside a group starts only that step.
- `d` on a group header skips all pending/failed steps in the group.
- `auto_run` works inside groups: pressing `R` on a group header starts all eligible steps and chains after the group completes.

## Key Bindings

| Key | Action |
|-----|--------|
| `↑` / `↓` | Navigate steps |
| `Tab` | Focus parameter inputs |
| `Shift+Tab` | Previous parameter input |
| `Esc` | Unfocus parameters / close modals |
| `r` | Run selected step |
| `R` | Run selected step and auto-run subsequent `auto_run` steps |
| `d` | Skip step (with confirmation) |
| `s` | Show session picker |
| `PgUp` / `PgDown` | Scroll stdout pane |
| `Home` / `End` | Jump to top/bottom of stdout |
| `q` / `Ctrl+C` | Quit |

### Session Picker

Press `s` to open the session picker:

| Key | Action |
|-----|--------|
| `↑` / `↓` | Navigate sessions |
| `Enter` | Load selected session |
| `n` | Create new session |
| `q` / `Esc` | Close picker |

## Session System

Sessions are automatically created and saved. Each session has a unique name based on the current datetime (`YYYY-MM-DD HH:MM:SS`).

**Auto-load rules on startup:**
- No previous session → create a new one
- Latest session has all steps done → create a new one
- Latest session has pending steps → resume that session

**Session storage:**
```
~/.local/share/tui-workflow/sessions/
  <cwd-hash>/
    <workflow-name>/
      <datetime>.json
```

Sessions are scoped by working directory and workflow name.

## Markdown Output

Steps can render their stdout as styled markdown by setting `output_type` to `"markdown"`:

```json
{
  "id": "readme",
  "name": "Generate README",
  "script": "scripts/markdown.sh",
  "output_type": "markdown",
  "description": "Generate a markdown README"
}
```

The output is rendered via glamour with a dark theme. Use `PgUp`/`PgDown` to scroll through the rendered content.

## Step Icons

In the step list, each step shows two icons:

- **Status icon**: `○` pending, `●` running, `✓` done, `✗` failed, `⊘` skipped
- **Run-type icon**: `↻` repeatable, `⊘` run-once per session, `⏵` auto-run step

Parallel groups are shown with a group header that displays the group name and an aggregate status icon. Individual steps inside the group are indented beneath the header.

## Development

```bash
go test -v
```

## License

MIT
