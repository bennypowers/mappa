# Agent instructions

Project conventions live in [CLAUDE.md](CLAUDE.md). This file records
cross-agent rules that apply regardless of which agent harness is driving.

## Tooling

Common CLI tools with no or few dependencies are assumed to be available:
`jq`, `yq`, `curl`, `git`, `gh`, `shellcheck`, `actionlint`. Use them directly
instead of writing language-runtime fallbacks, and do not add "if jq is
missing" branches. When a CI job needs one of these tools, install it in the
workflow rather than weakening the job.

Prefer `jq` and `yq` for JSON and YAML over inline `python -c` or hand-rolled
parsers.
