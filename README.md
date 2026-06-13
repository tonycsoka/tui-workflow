# TUI Workflow

A JSON-driven terminal UI for running sequenced, parameterised shell workflows.

> ⚠️ **Security Warning**: `tui-workflow` executes arbitrary shell scripts with your full user privileges. Only run workflow files from trusted sources.

## Features

- **JSON-driven workflows**: Define steps, parameters, and scripts in a simple JSON file.
- **Interactive parameter input**: Edit parameters in the TUI before running each step.
- **Sequential execution**: Steps unlock only after the previous step succeeds (or is skipped).
- **Session persistence**: Auto-saved, directory-aware sessions with unique datetime-based names. Resume or switch between sessions.
- **Live output**: Stream stdout/stderr from scripts in real-time.
- **Markdown output**: Steps can render their output as styled markdown via glamour.
- **Run-type indicators**: Steps show icons indicating whether they're repeatable (↻) or run-once (⊘).
- **Step info pane**: Shows description and last run time for the selected step.

## Installation

### From GitHub (latest)

```bash
go install github.com/yourusername/tui-workflow@latest
```

Then run it directly:

```bash
tui-workflow <workflow.json>
```

### From source

```bash
git clone https://github.com/yourusername/tui-workflow.git
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
- `steps` (array, required):
  - `id`: Unique step identifier.
  - `name`: Display name.
  - `script`: Path to shell script (relative to workflow JSON or absolute).
  - `params`: Array of parameter names to pass as positional arguments to the script.
  - `run_once_per_session`: If `true`, the step is skipped if it already succeeded in the current session.
  - `output_type`: Set to `"markdown"` to render the step's stdout as styled markdown.
  - `description`: Description shown in the step info pane.

## Key Bindings

| Key | Action |
|-----|--------|
| `↑` / `↓` | Navigate steps |
| `Tab` | Focus parameter inputs |
| `Shift+Tab` | Previous parameter input |
| `Esc` | Unfocus parameters / close modals |
| `r` | Run selected step |
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
- **Run-type icon**: `↻` repeatable, `⊘` run-once per session

## Development

```bash
go test -v
```

## License

MIT
