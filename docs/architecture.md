# Batuta architecture

## Purpose and boundaries

Batuta is a resource-only community extension: it orchestrates work but does
not implement code. It is an independent community project, not an official
or endorsed CompozyOS component.

## Components

Batuta packages one `batuta` agent, one `batuta-routing` skill, and one
`batuta-deliver` Loop. The bundled `spec-cycle` extension supplies the `cy-*`
skills plus `implement-tasks` and `review-and-fix`. CompozyOS owns sessions,
tool policy, durable Loop execution, and terminal effects.

## Data flow

```text
Operator
  -> Batuta session
  -> spec-cycle requirements and artifacts
  -> .compozy/tasks/$slug/task_*.md
  -> ext__spec_cycle__import_tasks preflight
  -> batuta-deliver
     -> implement-tasks
     -> review-and-fix
  -> compozy__session_prompt terminal return
  -> original Batuta conversation
```

## Preference and routing authority

Batuta reads the exact workspace `auto_commit` gate before dispatch. Provider
and model choices come from the live provider catalog, and the operator's
approved routing is stored as an `implement-tasks` override. Child Loops
resolve their own runtime rules.

## Resource and authority boundaries

Batuta never writes implementation code, approves its own gates, polls live
runs, pushes, or silently selects a commit preference. The operator approves
the relevant choices; executor Loops perform the implementation and review.

## Session lifecycle

Terminal callbacks return to the origin conversation. The current upstream UI
does not visually nest executor sessions, and normal terminal completion leaves
them `active/idle`.

## Trust and compatibility

For CompozyOS concepts and compatibility, use the [official documentation](https://www.compozy.com/docs/)
and [official repository](https://github.com/compozy/compozy). The manifest's
`0.3.0-beta.13` is a grammar floor; the verified full source commit is
`a35eda6d3a2ec47995c19a14a5a01d4f9452cf1c` for
`v0.3.0-beta.16-9-ga35eda6d`.
