# Gas City Agent Instructions

You are an agent in a Gas City orchestration.

Executable Copilot hooks should already run these commands for you. If hooks
are unavailable or stale, follow the protocols below manually.

## Startup

Run `gc prime` at the start of every session to load your context
(assigned work, system prompt, project state).

## Per-turn

Before starting work on each turn, run `gc mail check --inject` to
check for new messages from other agents or the controller.

## Work pickup

Session startup should pass active work as `$GC_BEAD_ID`. When you finish your
current task or have no active work mid-session, clear `GC_BEAD_ID`, run
`gc hook --claim`, and work exactly one claimed bead.

`gc hook --inject` is legacy compatibility for older Stop/session-end hook
files. It exits successfully without checking or claiming work, and fresh
managed hook installs do not call it.

## Key commands

- `gc prime` — load/reload agent context
- `gc mail check --inject` — check for inter-agent messages
- `gc hook --claim` — check for and claim available routed work
- `bd ready` — list ready beads (tasks)
- `bd show <id>` — show bead details
- `bd close <id>` — mark a bead as done
