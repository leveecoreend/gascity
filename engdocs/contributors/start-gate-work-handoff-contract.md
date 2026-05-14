# Start Gate Work Handoff Contract

This contract covers agents that claim their first bead before the runtime
session starts, then continue to claim more work from inside the live session.
The runtime contract is generic: a `start_gate` command may allow startup,
decline startup, or fail. The provider does not know whether the command
claimed work.

## Outcomes

| Situation | `gc hook --claim --start-gate` | Provider start | Reconciler action |
| --- | --- | --- | --- |
| Work is claimed | exit 0 and write `GC_BEAD_ID=<id>` to `$GC_START_ENV` | Start session with `GC_BEAD_ID` | Preserve the active bead through startup retries and quarantine |
| No work exists | exit 1 and write no env | Do not start session | Sleep without failure |
| A candidate row is stale | skip that row and keep scanning | Continue trying later candidates | No rollback because no claim was accepted |
| Claim conflict exhausts retries | exit 1 and write no env | Do not start session | Sleep without failure |
| Work query, store, or env-write failure | exit 2 | Treat as startup failure | Preserve pending create for retry; if `GC_BEAD_ID` was written, keep the same session/bead pairing |
| Command exits 1 after writing env | exit 1 with env | Treat as startup failure | Preserve the active bead; a claimed bead is not a clean decline |
| Runtime starts after claim, then later fails | exit 0 first | Startup failure | Existing wake-failure handling owns retry/quarantine; the claim remains assigned to the session |

## Env Handoff

`$GC_START_ENV` is a line-oriented env file. Each line is one environment
entry that the runtime validates and applies to the created process:

```sh
GC_BEAD_ID=gc-123
GC_ACTIVE_WORK_STATUS=claimed
```

The runtime validates the file before applying it. Invalid env names, invalid
values, duplicate keys, and oversized files are startup failures. Returned env
entries may override ambient env values for the created process.

`GC_BEAD_ID` is the prompt-facing active-work handoff. Claim ownership is by
canonical session identity (`GC_SESSION_ID`). `GC_SESSION_NAME` may still be
used for compatibility lookups, but new claims are assigned to
`GC_SESSION_ID`, not `GC_ALIAS` or the session name.

## Provider Ordering

Tmux and k8s must apply the same ordering:

1. Run `start_gate` with `$GC_START_ENV` set.
2. Read and validate any env file.
3. If the command exited 0, apply the env and run `pre_start`.
4. If the command exited 1 and wrote no env, decline startup cleanly.
5. Otherwise report startup failure, carrying the validated returned env map
   when present.
6. Run `pre_start` only after the gate succeeds. `pre_start` is setup-only.

Providers must not interpret claim-specific fields, maintain claim logs, or
attach claim rollback records to startup errors. They only report whether
startup may continue, declined cleanly, or failed for quarantine accounting.

When a role also needs expensive or persistent local setup, put the claim logic
in `start_gate` and the setup logic in `pre_start`. A clean start-gate decline
short-circuits `pre_start`, so no-work scale-ups do not create worktrees or
other disposable resources.

## Startup Failure Cleanup

The reconciler does not release claimed work from startup failure paths. That
would require it to know whether the start gate freshly claimed a bead or
merely revalidated work already assigned to the session. `GC_BEAD_ID` is the
active-work handoff field: when it is present and startup fails, the session
bead remains retryable with that active bead.

Clean start-gate declines sleep the session and clear only the pending-create
lease. They do not scan for or reopen `in_progress` work. Provider failures,
rate-limit holds, and wake-failure quarantine likewise preserve any active
assignment so the same session can retry after the failure condition clears.

If an operator intentionally closes a session bead while it still owns an
active `GC_BEAD_ID`, the work bead remains `in_progress` for that closed
session identity. Rescue is an operator action, not a provider rollback:
inspect work assigned to the closed session ID, then either restart that
session identity or explicitly reassign/reopen the work through `bd`.

Representative regressions are covered by
`TestRunPreparedStartCandidate_PreservesClaimBeforeTerminalCommit`,
`TestCommitStartResult_StartGateDeclinedUsesStableSleepReason`,
`TestCommitStartResult_RollbackPendingPreservesClaimAfterClose`, and
`TestCommitStartResult_RateLimitQuarantinePreservesClaim`.

## Continuation And Multiple Work Items

Start-gate claiming is only for the first bead in a new session. A live
session that closes a bead must clear `GC_BEAD_ID` and call `gc hook --claim`
again for additional work. In that post-close loop:

- exit 0 means work was claimed and should be verified before execution;
- exit 1 means no claimable work is currently available and the agent may poll
  briefly before drain-ack;
- any other nonzero exit is a hard failure and must not be treated as no work.

Continuation-group sibling assignment remains prompt/controller behavior. This
contract does not add an extra controller-side claim for continuation groups.
