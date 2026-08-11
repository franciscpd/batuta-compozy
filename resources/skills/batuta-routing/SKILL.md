---
name: batuta-routing
description: Default cost/complexity routing table for the batuta conductor. Read at bootstrap to seed per-workspace loop configuration; the stored workspace override is authoritative afterwards.
---

# Batuta Routing Table

Batuta's core opinion: route every task to the cheapest executor that can
handle it. Lanes use the `complexity` vocabulary that `cy-create-tasks`
writes into task frontmatter (`low`, `medium`, `high`, `critical`) — the
same vocabulary `runtime_rules[].match.complexity` matches on.

| Lane       | Runtime (`provider/model@reasoning`) | Intent                                  |
| ---------- | ------------------------------------ | --------------------------------------- |
| `low`      | `opencode/kimi-k2.5`                 | Contained change, cents per task        |
| `medium`   | `opencode/gpt-5.4`                   | New interfaces, moderate coordination   |
| `high`     | `opencode/gpt-5.4@high`              | New subsystem, heavy reasoning          |
| `critical` | `claude/claude-opus-4-8`             | Cross-cutting, high regression risk     |

## Canonical rules

This is the machine-readable form batuta applies with `compozy__loop_configure`
(stored per-workspace override for `implement-tasks`) during bootstrap, and the
form dispatches reuse as per-run `--runtime` rules. Precedence when both exist:
per-run > stored config. Rule matching precedence inside a layer: `id > type > complexity`.

```json runtime_rules
[
  {"match": {"complexity": "low"},      "runtime": {"provider": "opencode", "model": "kimi-k2.5"}},
  {"match": {"complexity": "medium"},   "runtime": {"provider": "opencode", "model": "gpt-5.4"}},
  {"match": {"complexity": "high"},     "runtime": {"provider": "opencode", "model": "gpt-5.4", "reasoning": "high"}},
  {"match": {"complexity": "critical"}, "runtime": {"provider": "claude",   "model": "claude-opus-4-8"}}
]
```

## Escalation and reclassification

- Repeated failure in a lane: re-dispatch the affected task with a per-run
  `id` rule one lane up (`--runtime id=task_NN:<runtime of the next lane>`).
  `id` beats `complexity`, so the override is surgical.
- Operator reclassification in conversation ("use kimi for this one") becomes
  the same per-run `id` rule on the next dispatch.
- The daemon persists `resolved_runtime` with per-field provenance on every
  generation — routing decisions are auditable via `compozy__loop_status`,
  never narrated.
