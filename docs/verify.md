# Verifying and installing Batuta

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

Pin a version with `github:franciscpd/batuta-compozy@v0.1.0-beta.3`.

## Manual path

If you prefer to inspect the archive before the daemon does:

```bash
version=0.1.0-beta.3
work=$(mktemp -d)
gh release download "v$version" --repo franciscpd/batuta-compozy --dir "$work"
(cd "$work" && sha256sum --check "batuta-v$version.tar.gz.sha256")
extracted=$(mktemp -d)
tar -xzf "$work/batuta-v$version.tar.gz" -C "$extracted"
compozy extension validate "$extracted" -o json
compozy extension install "$extracted" --allow-unverified --yes
compozy extension enable batuta
```

The release assets are exactly `batuta-v0.1.0-beta.3.tar.gz` and
`batuta-v0.1.0-beta.3.tar.gz.sha256`. The archive contains five files:
`LICENSE`, `extension.toml`, `agents/batuta/AGENT.md`,
`resources/skills/batuta-routing/SKILL.md`, `loops/batuta-deliver/loop.yaml`.

## Update and remove

```bash
compozy extension update batuta --allow-unverified --yes
compozy extension remove batuta --global
```

## Local development install

From a checkout, `scripts/republish.sh` checks the CompozyOS version, stages
the five package files into a temporary directory, validates them,
reinstalls and enables the extension, and checks the live inventory. See
`CONTRIBUTING.md`. A local install records the temporary staging path as its
source, so `compozy extension update` does not apply to it — run
`scripts/republish.sh` again instead.
