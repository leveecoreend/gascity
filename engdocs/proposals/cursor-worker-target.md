---
title: "Cursor Worker Target"
date: 2026-05-01
status: proposal
---

## Context

Gas City already carries a partial Cursor integration. `cursor` is a built-in
provider, appears in the init wizard, has hook overlay files, and can become an
implicit sling target when it is configured as a provider. The open question is
not whether Cursor requires a new orchestration primitive. It does not. Cursor
fits the existing Config + Agent Protocol + Task Store model.

The work is to turn the current preset into a verified worker target with the
same operational expectations as `claude`, `codex`, and `gemini`.

Cursor's current public CLI docs describe:

- interactive `cursor-agent` sessions, including an initial prompt argument
- non-interactive `-p` / `--print` mode, with `--output-format`
- `-f` / `--force` to allow commands unless explicitly denied
- `--resume [chatId]`, `cursor-agent resume`, and `cursor-agent ls`
- `cursor-agent status`, browser login, and `CURSOR_API_KEY`
- project rules from `.cursor/rules`, plus `AGENTS.md` and `CLAUDE.md`
- MCP support via Cursor's own MCP config surface

Sources:

- <https://docs.cursor.com/en/cli/overview>
- <https://docs.cursor.com/en/cli/using>
- <https://docs.cursor.com/en/cli/reference/parameters>
- <https://docs.cursor.com/en/cli/reference/authentication>
- <https://docs.cursor.com/en/cli/headless>
- <https://docs.cursor.com/en/cli/reference/output-format>

## Current State

### Built-in provider

`internal/worker/builtin/profiles.go` already includes:

```go
"cursor": {
    DisplayName:       "Cursor Agent",
    Command:           "cursor-agent",
    Args:              []string{"-f"},
    PromptMode:        "arg",
    ReadyPromptPrefix: "\u2192 ",
    ReadyDelayMs:      10000,
    ProcessNames:      []string{"cursor-agent"},
    SupportsHooks:     true,
    InstructionsFile:  "AGENTS.md",
},
```

`internal/config/provider_test.go` has `TestBuiltinProvidersCursor`, and the
init wizard test `TestRunWizardSelectCursorByNumber` asserts Cursor is exposed
as a selectable provider.

### Implicit target availability

`internal/config/config.go:InjectImplicitAgents` creates implicit agents for
configured providers. A provider is configured when either:

- `workspace.provider` names a built-in provider
- a `[providers.<name>]` table exists

That means a single-provider Cursor city already works through:

```toml
[workspace]
provider = "cursor"
```

To add Cursor alongside another default provider, configure it as an additional
provider:

```toml
[workspace]
provider = "claude"

[providers.codex]
base = "builtin:codex"

[providers.cursor]
base = "builtin:cursor"
```

After config load, implicit `claude`, `codex`, and `cursor` agents are
available as sling targets. A city may also define an explicit `[[agent]]`
named `cursor` and set `provider = "cursor"` if it wants custom prompt,
pool, workdir, or formula settings.

### Hooks

Cursor is included in `internal/hooks/hooks.go` and has an overlay file at
`internal/bootstrap/packs/core/overlay/per-provider/cursor/.cursor/hooks.json`.
The current events are `sessionStart`, `preCompact`, `beforeSubmitPrompt`, and
`stop`.

This needs live validation against current Cursor builds. Cursor's public hooks
surface has changed recently enough that the shipped event names should be
treated as an assumption until a conformance fixture or smoke test proves them.

### Readiness

`internal/api/handler_provider_readiness.go` only probes `claude`, `codex`,
and `gemini`. Cursor is not part of:

- `defaultProviderReadinessItems`
- `supportedProviderReadiness`
- `readinessProbeSpecs`

`gc init` will warn that Cursor's login state cannot be automatically verified.
Cursor docs expose `cursor-agent status`, browser login, API-key auth, and
`CURSOR_API_KEY`, so a built-in probe is feasible.

### Session resume and one-shot mode

Cursor currently lacks:

- `ResumeFlag`
- `ResumeStyle`
- `SessionIDFlag`
- `PrintArgs`
- `OptionsSchema`
- `TitleModel`

Cursor docs expose `--resume [chatId]`, `cursor-agent resume`, `cursor-agent
ls`, and `-p` / `--print`. That is enough to add resume and one-shot support
after live CLI behavior is verified.

### Transcript and worker conformance

Cursor is not a canonical worker profile today.

Only these profiles exist in `internal/worker/workertest/profiles.go` and
`internal/worker/builtin/profiles.go:CanonicalProfileIdentity`:

- `claude/tmux-cli`
- `codex/tmux-cli`
- `gemini/tmux-cli`

Transcript discovery currently maps unknown providers to Claude-style session
lookup:

- `internal/worker/transcript/discovery.go`
- `internal/sessionlog/reader.go`

That is not a certified Cursor transcript adapter. It may work only if Cursor
happens to share Claude-like local JSONL files, which is not established in
this codebase or the public Cursor CLI docs.

### MCP and skills

Cursor is not in the active MCP projection list. `cmd/gc/mcp_integration.go`
supports only Claude, Codex, and Gemini provider families, and
`internal/materialize/mcp_project.go` rejects `cursor`.

Cursor also has no skill sink in `internal/materialize/skills.go`. That file
intentionally materializes skills only for providers with verified skill-reading
behavior.

Cursor docs say the CLI detects Cursor MCP configuration and reads
`AGENTS.md`, `CLAUDE.md`, and `.cursor/rules`. That supports follow-up work,
but it should not block the minimal worker target.

## Proposal

Ship Cursor in three slices.

### Slice 1: verified basic target

Goal: `gc sling cursor "..."` works in a configured city and launches a durable
interactive Cursor worker with normal Gas City prompt and hook behavior.

Changes:

1. Add docs showing the config-only path for Cursor as a primary provider and
   as an additional provider alongside `claude` / `codex`.
2. Install `cursor-agent` in a local acceptance environment and verify the
   current preset:
   - `cursor-agent -f "<prompt>"` starts an interactive long-running session
   - prompt submission works with `PromptMode = "arg"`
   - U+2192 followed by a space is still the ready prompt prefix
   - `cursor-agent` remains visible under the configured process name
   - `.cursor/hooks.json` is loaded and all configured hook events fire
3. If the live CLI rejects positional prompts or the ready prefix has changed,
   update the built-in provider spec and tests.
4. Add a narrow startup/runtime test fixture for Cursor launch command
   rendering, mirroring the current provider tests.

Acceptance:

- `gc init --provider cursor` produces a usable city.
- A city with `[providers.cursor] base = "builtin:cursor"` exposes an implicit
  `cursor` target.
- `gc sling cursor "..."` starts or reuses a Cursor session in tmux.
- Existing Cursor provider tests still pass.

### Slice 2: operational parity

Goal: Cursor has the same day-two operator affordances as the existing primary
providers.

Changes:

1. Add Cursor readiness support:
   - detect `cursor-agent`
   - run `cursor-agent status` with a short timeout
   - report `configured`, `needs_auth`, `not_installed`, or `probe_error`
   - include install/auth hints for `cursor-agent login` and `CURSOR_API_KEY`
2. Add provider options:
   - `permission_mode` defaulting to the current `-f` behavior
   - `model` mapped to `-m` / `--model`
   - optionally `output_format` for one-shot use only
3. Move hard-coded `-f` toward an option default if the runtime option plumbing
   can express it cleanly. If not, keep `Args: ["-f"]` for backward behavior and
   make the limitation explicit in docs.
4. Add one-shot support with `PrintArgs: []string{"-p"}` for title generation
   and automation paths that expect a provider to print and exit.
5. Add resume support after live behavior is confirmed:
   - if `--resume <id>` works with the normal interactive command, use
     `ResumeFlag = "--resume"` and `ResumeStyle = "flag"`
   - if only `cursor-agent resume <id>` works, use a `ResumeCommand`
   - do not set `SessionIDFlag` unless Cursor supports caller-supplied thread
     IDs

Acceptance:

- provider readiness endpoints accept `cursor`
- `gc init` no longer emits the "cannot verify login state" warning for Cursor
- restart/resume either preserves Cursor thread continuity or emits an explicit
  unsupported-resume diagnostic
- one-shot title generation can use Cursor when configured

### Slice 3: certified first-class worker profile

Goal: Cursor is covered by the worker conformance framework rather than
treated as a best-effort custom provider.

Changes:

1. Capture Cursor session fixtures from a live Cursor CLI:
   - fresh session
   - continuation/resume session
   - reset/fresh isolation session
2. Identify Cursor's durable transcript source:
   - local transcript file path and schema, if one exists
   - otherwise whether `stream-json` output can serve only one-shot paths, not
     long-running tmux session history
3. Add a Cursor transcript adapter:
   - provider-specific discovery
   - provider-specific reader
   - raw transcript support
   - pagination and display filtering as needed
4. Add `cursor/tmux-cli` to:
   - `internal/worker/workertest/profiles.go`
   - `internal/worker/builtin/profiles.go:CanonicalProfileIdentity`
   - fixture directories under `internal/worker/workertest/testdata/fixtures`
5. Extend conformance tests so Cursor proves:
   - transcript normalization
   - continuation continuity
   - fresh session isolation

Acceptance:

- `cursor/tmux-cli` has a stable compatibility fingerprint.
- Cursor appears in the canonical worker profile list.
- Cursor transcript and continuity tests pass without falling through to the
  Claude reader.

## Explicit Non-Goals

- No new primitive.
- No provider-specific decision logic beyond provider launch, readiness,
  transcript, hook, MCP, and skill delivery surfaces.
- No hardcoded agent role behavior.
- No MCP projection in Slice 1.
- No skill materialization in Slice 1.
- No switch to `cursor-agent -p` as the default worker launch path. Print mode
  exits and does not satisfy the long-running tmux worker contract.

## Risks and Open Questions

1. **Interactive contract.** The existing built-in assumes `cursor-agent -f
   "<prompt>"` starts an interactive session. Cursor docs show interactive
   initial prompts, but the exact interaction with `-f` and the ready prefix
   must be verified against the current binary.
2. **Hook schema drift.** The shipped `.cursor/hooks.json` may not match the
   current Cursor hooks vocabulary or load path.
3. **Transcript source.** Public Cursor docs document thread resume, but not a
   local transcript schema. This is the biggest blocker for certification.
4. **Resume key ownership.** Cursor exposes chat IDs, but it is not clear that
   Gas City can generate and pass a session ID up front.
5. **MCP target.** Cursor CLI supports MCP, but Gas City's MCP projector needs a
   verified Cursor-native target and merge/ownership semantics before enabling
   it.
6. **Auth in containers.** Browser login is awkward for remote rigs. Cursor's
   `CURSOR_API_KEY` path should be the recommended non-interactive rig setup.

## Recommended Implementation Order

1. Land a small docs update for configuring Cursor as an additional provider.
2. Run a live CLI spike and update the built-in Cursor provider spec if needed.
3. Add readiness probing through `cursor-agent status`.
4. Add print/resume/options support with tests.
5. Add transcript discovery and conformance.
6. Add Cursor MCP projection and skill/rules materialization only after the
   basic worker path is proven.

The first two steps are enough to answer "can Cursor be a target alongside
Codex and Claude?" The later steps answer "can Cursor be treated as a
first-class certified worker profile?"
