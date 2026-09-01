# Verifying and installing Batuta

The published remote release remains `v0.1.0-beta.3`; `v0.1.0-beta.6` is the
next candidate and is not a tag or release yet. Its compatibility baseline is
the official Compozy Go SDK `v0.3.0-beta.21`; source commit
`382976d4b43274630a4b67445812fd4a0216dbcc` is only the build/lint baseline.

The public runtime floor remains `v0.3.0-beta.21`. A source build that identifies
itself as beta.20 remains below that floor even when its source includes some
later contracts; lint success alone is not runtime compatibility.

## What `--allow-unverified` means

Direct GitHub installs are in CompozyOS's `unverified` registry tier. The live
policy `extensions.trust.allow_unverified` and your explicit
`--allow-unverified` consent are required; `--yes` only skips the confirmation.
The flag does not disable integrity checks. Inspect provenance after installation:

```bash
compozy extension provenance batuta -o json
```

Expected GitHub provenance includes `installed_from: "github"` and, when the
release provides an archive sidecar, `digest_matched: true`.

## Install from GitHub

```bash
compozy extension install github:franciscpd/batuta-compozy --allow-unverified --yes
compozy extension enable batuta
```

The current published pin is
`github:franciscpd/batuta-compozy@v0.1.0-beta.3`. Do not claim beta.6 as
installable until its remote tag and release exist.

```bash
compozy extension update batuta --allow-unverified --yes
compozy extension remove batuta --global
```

## Candidate and local development verification

`scripts/stage-extension.sh` creates an immutable candidate generation. Its
staged Go sources include `go.mod`, `go.sum`, the `batuta` agent, the
`batuta-routing` skill, and all three Loop files:

```text
agents/batuta/AGENT.md
resources/skills/batuta-routing/SKILL.md
loops/batuta-deliver/loop.yaml
loops/batuta-deliver-core/loop.yaml
loops/batuta-task/loop.yaml
```

It deliberately excludes repository plans, specs, QA reports, and SDD artifacts
from the extension package. Build and validate the staged generation, not the
source checkout:

```bash
stage=$(mktemp -d)
scripts/stage-extension.sh "$stage"
compozy extension build "$stage" -o json
compozy extension validate "$stage" -o json
```

For a complete local install, use `scripts/republish.sh` against an isolated
`COMPOZY_HOME`. It stages the same Go source generation, validates it, installs
it, and requires this exact inventory: one `batuta` agent, public
`batuta-deliver`, internal `batuta-deliver-core`, and `batuta-task` Loops,
`batuta-routing`, and all nine hosted Batuta tools, including
`ext__batuta__delivery_graph`.

The exact source pin is a development compatibility floor; validate the public
surface of the daemon you will operate. A local path install cannot use
`compozy extension update`; rebuild and reinstall the validated generation.

Provider/model identity comes only from the visible Compozy catalog. A directly
live pair is executable; a visible `unknown` pair becomes executable only when
an available dedicated CLI adapter proves that exact provider/model pair.
Provider-only proof, adapter-only models, stale rows, and unavailable rows stay
ineligible. The extension probes Codex, OpenCode, Cursor Agent, Claude Code,
and Agy without logging in, installing, refreshing configuration, or calling
Agy's authenticated network-backed `models` command.

Frontend routing prefers exact eligible pairs in this order: Cursor/Grok 4.6,
Cursor/Claude Opus 5, then Codex/GPT-5.6 Terra with `high` reasoning. Missing
members are skipped; no display alias is reconstructed into a runtime ID.

## Release verification

The release workflow accepts only a full candidate SHA and an unused beta SemVer.
It verifies the candidate through CI before an annotated tag, stages the same
immutable package, publishes it, then re-installs from GitHub and checks
inventory/provenance. It never packages source-only plans or swaps a live
workspace daemon for a release check.
