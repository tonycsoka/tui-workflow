#!/bin/bash
# Produces markdown output with tables and formatting

cat <<'EOF'
# Deployment Report

## Summary

This report is rendered as **markdown** in the TUI! The `output_type` field
triggers glamour rendering with a dark theme.

## Features Demonstrated

- **Bold text** and *italic text*
- `Inline code` blocks
- Tables with alignment
- Blockquotes
- Bullet lists

## Environment Status

| Service    | Status  | Uptime |
|-----------|---------|--------|
| Database  | ✅ Up   | 99.9%  |
| API       | ✅ Up   | 99.8%  |
| Worker    | 🐌 Slow | 95.2%  |
| Cache     | ✅ Up   | 99.9%  |

## Code Example

```bash
#!/bin/bash
./tui-workflow examples/full-demo.json
```

> This step is marked as **run-once per session** (⊘). After it succeeds,
> it will be skipped automatically in future runs of the same session.

EOF
