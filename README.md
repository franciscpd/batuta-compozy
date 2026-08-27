# batuta-compozy

> 🇧🇷 [Versão em português](README.pt-BR.md)

Batuta is a conductor agent for [CompozyOS](https://www.compozy.com/docs/).
You describe the outcome; Batuta writes the SDD through the bundled
`spec-cycle`, creates tasks, performs automatic executor inventory, classifies
each task by domain × complexity, archives an immutable runtime matrix, and
owns a bounded delivery across fresh Compozy runs. It never implements feature
code itself.

Delivery uses `auto_commit=true`, bounded fresh-run fallback, independent
review, and automatic exact-HEAD publication with no human publication gate on
the healthy path. Push and PR creation are automatic; merge remains manual.
The operator is involved only when product requirements are missing or an
external prerequisite such as credentials or a remote is genuinely blocked.

Batuta is an independent community project, not an official or endorsed
CompozyOS component. CompozyOS lives at
[github.com/compozy/compozy](https://github.com/compozy/compozy).

![Batuta in CompozyOS](docs/images/batuta-no-compozy.png)

```text
conversation → SDD → tasks → inventory → domain × complexity matrix
                                      ↓
report ← verify PR ← publish ← review ← attempt 1 in isolated worktree
                                      ↓ failed task only
                                  attempt 2 (fresh run)
```

## Install

The published `v0.1.0-beta.4` remains the current GitHub release. This branch
targets `v0.1.0-beta.5`. The official `v0.3.0-beta.21` Go SDK is used directly,
with no `replace` or fork dependency. Public beta.5 promotion still waits for
an official Compozy binary release containing child `run-loop`
`config_overrides`; the compatible local preview is supported for development.

Prerequisites:

- a compatible CompozyOS daemon;
- the bundled `spec-cycle` extension enabled;
- at least one authenticated model in `compozy provider models list`.

```bash
compozy extension install github:franciscpd/batuta-compozy --allow-unverified --yes
compozy extension enable batuta
```

`--allow-unverified` is explicit consent for a community source; archive
integrity verification still applies. See [docs/verify.md](docs/verify.md).

Update: `compozy extension update batuta --allow-unverified --yes` ·
Remove: `compozy extension remove batuta --global`

Current published release: [`v0.1.0-beta.4`](docs/releases/0.1.0-beta.4.md).
Next candidate: [`v0.1.0-beta.5`](docs/releases/0.1.0-beta.5.md).

## Use

Open a Compozy session with the `batuta` agent in your project workspace and
describe the change. Batuta asks only requirement questions, then:

1. runs `cy-create-spec` and waits for approval of the product contract;
2. runs `cy-create-tasks` and validates canonical task metadata;
3. inventories Compozy, Codex, OpenCode, and Cursor Agent without exposing
   secrets;
4. chooses exact provider/model IDs present in the live Compozy catalog;
5. archives exact `type + complexity` rules without mutating stored Loop config;
6. starts `batuta-deliver` attempt 1 with cap 4 and one 14,400-second deadline;
7. implements, reviews, retries failed tasks in a fresh run within the original budget, commits, and
   opens one PR for the delivery phase.

For example, a frontend cell can select Cursor Agent with the exact live ACP
ID `cursor/grok-4.6[effort=high,fast=true]`, while a backend cell selects a
different executor/model. Foreign executor configuration informs capability
selection, but only exact bindings reported by Compozy can execute.

The complete contracts are in [docs/how-it-works.md](docs/how-it-works.md) and
[docs/architecture.md](docs/architecture.md). The presentation roadmap is
[docs/images/batuta-next-roadmap.png](docs/images/batuta-next-roadmap.png).

## Known limitations

- Public beta.5 promotion awaits an official Compozy binary containing child
  `run-loop` `config_overrides`; local validation uses a compatible preview.
- executor sessions are not visually nested and remain active/idle after normal
  terminal completion.
- A missing forge provider, remote, or credential is reported as a blocker;
  Batuta never treats a compare URL as a published PR.

## Learn more

- [How it works](docs/how-it-works.md) · [Verify and install](docs/verify.md)
- [Architecture](docs/architecture.md) ·
  [Case study](docs/case-studies/version-subcommand.md)
- [Contributing](CONTRIBUTING.md) · [MIT license](LICENSE)
