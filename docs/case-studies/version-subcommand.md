# Version-subcommand case study

This case study records one bounded, reproducible Batuta delivery journey. It
describes observed outcomes, not a benchmark or a claim about every workload.

## Question

The sanitized request was: “Add a minimal CLI feature that preserves the
literal requirement `todo 1.0.0`. Do not write code before the specification
and tasks are approved.”

## Environment

The run used a Batuta beta.2 candidate with compatible CompozyOS source commit
`a35eda6d3a2ec47995c19a14a5a01d4f9452cf1c`, bundled `spec-cycle` 0.4.0, and
a clean fixture repository. This case study deliberately omits provider,
model, and machine identity.

## Preference gate

The exact workspace preference key was absent. The operator chose false, and
Batuta persisted and reread `auto_commit=false` before it began planning.

## Specification

`cy-create-spec` produced `_spec.md`, `_user_stories.md`, `_dx.md`, and
`_tests.md`. No `_uiux.md` was needed because the request had no Web surface.

## Tasks and preflight

`cy-create-tasks` produced one backend task. A direct
`ext__spec_cycle__import_tasks` call returned a positive task count, and the
literal `todo 1.0.0` requirement survived unchanged.

## Delivery

A dry-run preceded the real `batuta-deliver` dispatch. The composite delivery
then invoked `implement-tasks` and, after that, `review-and-fix`. All three
runs reached the exact terminal state `done`.

## Observable result

Only `README.md`, `src/cli.py`, and `tests/test_cli.py` changed. The fixture
reported 9/9 tests passing. No commit was created because `auto_commit=false`.

## What this proves

This single journey demonstrates bounded orchestration, literal requirement
preservation, implementation-before-review ordering, and event-driven terminal
return.

## What this does not prove

It does not establish general performance, cost, provider superiority, or
stable compatibility. The existing upstream limitation remains:
executor sessions are not visually nested and remain active/idle after normal
terminal completion.
This journey does not prove automatic session nesting or automatic
executor-session stop.

## Reproduce

Start with the public [README](../../README.md), [architecture guide](../architecture.md),
and [beta.2 release notes](../releases/0.1.0-beta.2.md). Inspect the bundled
[delivery Loop](../../loops/batuta-deliver/loop.yaml) and
[routing skill](../../resources/skills/batuta-routing/SKILL.md), then use the
[v0.1.0-beta.2 release](https://github.com/batuta-ai/compozy/releases/tag/v0.1.0-beta.2).
The public task-artifact pattern is `.compozy/tasks/$slug/task_*.md`.
