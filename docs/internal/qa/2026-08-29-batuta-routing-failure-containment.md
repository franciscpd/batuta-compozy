# Routing failure containment — local QA

Date: 2026-08-30

Status: deterministic boundaries `pass`; isolated direct wire qualification
`pass`; bounded model-facing qualification `pass`.

## Scope

This qualification covers two previously unsafe routing failure paths:

- CLI tool fallbacks preserve the Batuta agent scope with `--agent batuta`;
- `routing_apply` exposes the same actionable domain rejection as
  `routing_plan`, instead of allowing Compozy to normalize it as
  `backend_unhealthy`.

The Batuta contract now treats a successful `routing_plan` response as the
only digest authority. A rejected plan permits at most one materially corrected
retry. A second rejection, or an operator constraint that prevents a corrected
retry, is terminal with zero `routing_apply` calls and zero mutation.

The qualified Batuta source was
`b658f22594d49608015f534fb79b78959cda4db0`. The runtime was the local
Compozy preview `0.3.0-beta.21.preview.738edb54f` at commit `738edb54f`.
The staged extension generation was
`09de39634b75dc60797115318246edcb9a9389c1d4c34c91d8402e3bba250e22`.

No shared Compozy source, configuration, database, extension installation, or
daemon state was changed. The qualification used a unique `COMPOZY_HOME`,
`XDG_CACHE_HOME`, daemon socket and HTTP port, disposable non-Git workspace,
and staged Batuta installation. Teardown removed only those owned resources.

## Direct wire qualification

The disposable `parallel-demo` fixture contained five pending task artifacts.
Every fit proposal deliberately used the live catalog pair
`cursor/composer-2.5[fast=true]`; this standard-tier model is below the medium
and high complexity floors.

The direct `ext__batuta__routing_plan` invocation returned exit 1 with:

```json
{
  "code": "tool_invalid_input",
  "message": "invalid tool request",
  "tool_id": "ext__batuta__routing_plan",
  "reason_codes": ["routing_fit_retryable", "model_below_floor"]
}
```

The same rejected plan was then submitted to
`ext__batuta__routing_apply` with operation `alignment_status` and a
syntactically valid decoy digest. It returned the same exit code and structured
reason codes. It did not return `backend_unhealthy`.

Snapshots taken before the plan and after the apply proved:

- workspace file names and contents unchanged;
- `.git` absent before and after;
- isolated routing journal unchanged;
- zero Loop runs;
- Batuta extension PID stable and health still `active/healthy`.

## Bounded model-facing qualification

One Batuta session used the live runtime
`cursor/composer-2.5[fast=true]` with a 90-second outer bound. The operator
prompt restricted every task to that runtime and explicitly prohibited
`routing_apply`, repository bootstrap, extension lifecycle operations, and
workspace mutation.

The session read the routing skill and task artifacts, collected the executor
inventory, read `tool_info` once, and invoked `routing_plan` once. It received
`routing_fit_retryable` plus `model_below_floor`, reported the blocker, and
ended `idle/done` in about 35 seconds.

The persisted tool-call history proved:

```text
routing_plan calls: 1
routing_apply calls: 0
bootstrap_repository calls: 0
extension lifecycle calls: 0
tool_info calls: 1
```

Post-session snapshots again proved unchanged workspace contents, absent Git,
unchanged routing journal, and zero Loop runs. No digest was invented and no
fallback mutation was attempted.

## Fresh deterministic gates

```text
rtk go test ./... -count=1
PASS: 578 tests in 9 packages.

rtk go test -race ./internal/extensionapp -run 'RoutingPlan|RoutingApply' -count=1
PASS: 17 tests.

rtk go vet ./...
PASS.

rtk bash -n tests/contract/*.sh scripts/*.sh
PASS.

rtk bash tests/contract/test_02_domain_lane_surface.sh
PASS.

rtk git diff --check
PASS.
```

The final completion gate repeats these checks after the QA record is added.
