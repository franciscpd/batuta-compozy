# Verifying and installing Batuta

The GitHub instructions below describe the current published
`v0.1.0-beta.4`. The `v0.1.0-beta.5` candidate changes the package to a
code-backed Linux/amd64 generation and uses the official Compozy Go SDK
`v0.3.0-beta.21` directly. It has no `replace`, pseudo-version, or fork-only
dependency. Public beta.5 promotion still waits for an official Compozy binary
release containing child `run-loop` `config_overrides`; current end-to-end
validation uses an isolated compatible preview.

## What `--allow-unverified` means

CompozyOS installs extensions from a curated catalog, from GitHub, from a git
URL, or from a local path. Anything that is not an official or community
catalog entry — including a direct GitHub install like Batuta — is in the
`unverified` registry tier and needs two things: the live policy
`extensions.trust.allow_unverified` (default `true`) and your explicit
`--allow-unverified` on the command (`--yes` skips the confirmation prompt).
That flag is the whole consent; it does not disable integrity checks.

Every Batuta release carries a `.sha256` sidecar next to its archive. When
the daemon finds it, it verifies the archive against that digest before
extraction and records `digest_matched: true` in the extension's provenance.
Inspect the decision at any time:

```bash
compozy extension provenance batuta -o json
```

Expected for a GitHub install: `installed_from: "github"`,
`digest_matched: true`, `registry_tier: "unverified"`.

## Install from GitHub (recommended)

```bash
compozy extension install github:franciscpd/batuta-compozy --allow-unverified --yes
compozy extension enable batuta
```

Pin a version with `github:franciscpd/batuta-compozy@v0.1.0-beta.4`.

## Manual path

If you prefer to inspect the archive before the daemon does:

```bash
version=0.1.0-beta.4
work=$(mktemp -d)
gh release download "v$version" --repo franciscpd/batuta-compozy --dir "$work"
(cd "$work" && sha256sum --check "batuta-v$version.tar.gz.sha256")
extracted=$(mktemp -d)
tar -xzf "$work/batuta-v$version.tar.gz" -C "$extracted"
compozy extension validate "$extracted" -o json
compozy extension install "$extracted" --allow-unverified --yes
compozy extension enable batuta
```

The release assets are exactly `batuta-v0.1.0-beta.4.tar.gz` and
`batuta-v0.1.0-beta.4.tar.gz.sha256`. The archive contains six files:
`LICENSE`, `extension.toml`, `agents/batuta/AGENT.md`,
`agents/batuta-publisher/AGENT.md`, `resources/skills/batuta-routing/SKILL.md`,
`loops/batuta-deliver/loop.yaml`.

## Update and remove

```bash
compozy extension update batuta --allow-unverified --yes
compozy extension remove batuta --global
```

## Local development install

For beta.5, `scripts/republish.sh` checks the beta.21 runtime floor, stages
only production Go source plus the declared agent/skill/Loop resources, runs
`compozy extension build`, validates that immutable generated directory, and
installs the same generation it validated. Its live inventory must contain
the agent, Loop, skill, and all eight hosted Batuta tools. The source directory
is never installed as a resource-only fallback.

The script deliberately builds with `GOWORK=off` against the official SDK.
For the current local lab, run it with the compatible preview binary, validate
the returned `generation_dir`, and install that exact directory. See
`CONTRIBUTING.md`.
A local install records a local path, so `compozy extension update` does not
apply to it — rebuild and reinstall instead.
